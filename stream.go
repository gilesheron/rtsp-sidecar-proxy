package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aler9/gortsplib"
	"gortc.io/sdp"
)

const (
	_DIAL_TIMEOUT          = 10 * time.Second
	_RETRY_INTERVAL        = 5 * time.Second
	_CHECK_STREAM_INTERVAL = 6 * time.Second
	_STREAM_DEAD_AFTER     = 5 * time.Second
	_KEEPALIVE_INTERVAL    = 60 * time.Second
)

type streamUdpListenerPair struct {
	udplRtp  *streamUdpListener
	udplRtcp *streamUdpListener
}

type streamState int

const (
	_STREAM_STATE_STARTING streamState = iota
	_STREAM_STATE_READY
)

type stream struct {
	p               *program
	state           streamState
	path            string
	ur              *url.URL
	proto           streamProtocol
	clientSdpParsed *sdp.Message
	serverSdpText   []byte
	serverSdpParsed *sdp.Message
	clientAnnounce  bool
	firstTime       bool
	terminate       chan struct{}
	done            chan struct{}
	conn            *gortsplib.ConnClient
}

// GetLocalIP returns the non loopback local IP of the host
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func assignHost(ur *url.URL) error {

	// handle local requests
	if ur.Hostname() == getLocalIP() {
		ur.Host = "127.0.0.1:554"
		return nil
	}

	lb, err := NewRoundRobinLB(ur.Hostname())
	if err != nil {
		return fmt.Errorf("could not retrieve service endpoints from clusterIP: %v", err)
	}

	// map to specific k8s service endpoint
	endpoint, err := MapToEndpoint(lb, ur.Hostname())

	if err != nil {
		return err
	}

	ur.Host = fmt.Sprintf("%s:8554", strings.Split(endpoint, ":")[0])
	return nil
}

func newStream(p *program, path string, ur *url.URL, proto streamProtocol, clientAnnounce bool, clientSdpParsed *sdp.Message) (*stream, error) {

	// load balance rtsp endpoints
	err := assignHost(ur)

	if err != nil {
		log.Printf("Error occured in stream host setup: %s", err.Error())
		return nil, err
	}

	log.Printf("New stream host set to: %s", ur.Host)

	if ur.Scheme != "rtsp" {
		return nil, fmt.Errorf("unsupported scheme: %s", ur.Scheme)
	}
	if ur.User != nil {
		pass, _ := ur.User.Password()
		user := ur.User.Username()
		if user != "" && pass == "" ||
			user == "" && pass != "" {
			fmt.Errorf("username and password must be both provided")
		}
	}

	s := &stream{
		p:               p,
		state:           _STREAM_STATE_STARTING,
		path:            path,
		ur:              ur,
		proto:           proto,
		firstTime:       true,
		clientAnnounce:  clientAnnounce,
		clientSdpParsed: clientSdpParsed,
		terminate:       make(chan struct{}),
		done:            make(chan struct{}),
	}

	return s, nil
}

func (s *stream) log(format string, args ...interface{}) {
	format = "[STREAM " + s.path + "] " + format
	log.Printf(format, args...)
}

func (s *stream) run() {
	for {
		ok := s.do()
		if !ok {
			break
		}
	}

	close(s.done)
}

func (s *stream) do() bool {
	if s.firstTime {
		s.firstTime = false
	} else {
		t := time.NewTimer(_RETRY_INTERVAL)
		select {
		case <-s.terminate:
			return false
		case <-t.C:
		}
	}

	s.log("initializing with protocol %s", s.proto)

	var nconn net.Conn
	var err error
	dialDone := make(chan struct{})
	go func() {
		nconn, err = net.DialTimeout("tcp", s.ur.Host, _DIAL_TIMEOUT)
		close(dialDone)
	}()

	select {
	case <-s.terminate:
		return false
	case <-dialDone:
	}

	if err != nil {
		s.log("Dial ERR: %s", err)
		return true
	}
	defer nconn.Close()

	conn, err := gortsplib.NewConnClient(gortsplib.ConnClientConf{
		NConn: nconn,
		Username: func() string {
			if s.ur.User != nil {
				return s.ur.User.Username()
			}
			return ""
		}(),
		Password: func() string {
			if s.ur.User != nil {
				pass, _ := s.ur.User.Password()
				return pass
			}
			return ""
		}(),
		ReadTimeout:  s.p.readTimeout,
		WriteTimeout: s.p.writeTimeout,
	})
	if err != nil {
		s.log("Connection ERR: %s", err)
		return true
	}

	s.log("Created connection to %s", conn.NetConn().RemoteAddr().String())

	res, err := conn.WriteRequest(&gortsplib.Request{
		Method: gortsplib.OPTIONS,
		Url: &url.URL{
			Scheme: "rtsp",
			Host:   s.ur.Host,
			Path:   "/",
		},
	})
	if err != nil {
		s.log("OPTIONS ERR: %s", err)
		return true
	}

	s.log("Sent OPTIONS to server, response %s", res.StatusMessage)

	// OPTIONS is not available in some cameras
	if res.StatusCode != gortsplib.StatusOK && res.StatusCode != gortsplib.StatusNotFound {
		s.log("ERR: OPTIONS returned code %d (%s)", res.StatusCode, res.StatusMessage)
		return true
	}

	// Init vars here so we can have common mutex lock after the if/else
	clientSdpParsed := s.clientSdpParsed
	serverSdpParsed := s.serverSdpParsed
	serverSdpText := s.serverSdpText

	if s.clientAnnounce == true {
		// create a filtered SDP that is sent to the server
		serverSdpParsed, serverSdpText = gortsplib.SDPFilter(clientSdpParsed, res.Content)

		res, err = conn.WriteRequest(&gortsplib.Request{
			Method: gortsplib.ANNOUNCE,
			Url: &url.URL{
				Scheme:   "rtsp",
				Host:     s.ur.Host,
				Path:     s.ur.Path,
				RawQuery: s.ur.RawQuery,
			},
			Header: gortsplib.Header{
				"Content-Type":   []string{"application/sdp"},
				"Content-Length": []string{strconv.Itoa(len(serverSdpText))},
			},
			Content: serverSdpText,
		})

		if err != nil {
			s.log("ANNOUNCE ERR: %s", err)
			return true
		}

		if res.StatusCode != gortsplib.StatusOK {
			s.log("ERR: ANNOUNCE returned code %d (%s)", res.StatusCode, res.StatusMessage)
			return true
		}

	} else {
		res, err = conn.WriteRequest(&gortsplib.Request{
			Method: gortsplib.DESCRIBE,
			Url: &url.URL{
				Scheme:   "rtsp",
				Host:     s.ur.Host,
				Path:     s.ur.Path,
				RawQuery: s.ur.RawQuery,
			},
		})

		if err != nil {
			s.log("DESCRIBE ERR: %s", err)
			return true
		}

		s.log("Sent DESCRIBE to Server, response %s", res.StatusMessage)

		if res.StatusCode != gortsplib.StatusOK {
			s.log("ERR: DESCRIBE returned code %d (%s)", res.StatusCode, res.StatusMessage)
			return true
		}

		contentType, ok := res.Header["Content-Type"]
		if !ok || len(contentType) != 1 {
			s.log("ERR: Content-Type not provided")
			return true
		}

		if contentType[0] != "application/sdp" {
			s.log("ERR: wrong Content-Type, expected application/sdp")
			return true
		}

		clientSdpParsed, err = gortsplib.SDPParse(res.Content)
		if err != nil {
			s.log("ERR: invalid SDP: %s", err)
			return true
		}

		// create a filtered SDP that is used by the server (not by the client)
		serverSdpParsed, serverSdpText = gortsplib.SDPFilter(clientSdpParsed, res.Content)
	}

	// update the stream
	func() {
		s.p.tcpl.mutex.Lock()
		defer s.p.tcpl.mutex.Unlock()

		s.clientSdpParsed = clientSdpParsed
		s.serverSdpText = serverSdpText
		s.serverSdpParsed = serverSdpParsed
		s.conn = conn
	}()

	s.log("running stream with protocol %s", s.proto)

	if s.proto == _STREAM_PROTOCOL_UDP {
		return s.runUdp(conn, s.clientAnnounce)
	} else {
		return s.runTcp(conn, s.clientAnnounce)
	}
}

func (s *stream) runUdp(conn *gortsplib.ConnClient, clientAnnounce bool) bool {
	publisherIp := conn.NetConn().RemoteAddr().(*net.TCPAddr).IP

	var streamUdpListenerPairs []streamUdpListenerPair

	defer func() {
		for _, pair := range streamUdpListenerPairs {
			pair.udplRtp.close()
			pair.udplRtcp.close()
		}
	}()

	for i, media := range s.clientSdpParsed.Medias {
		var rtpPort int
		var rtcpPort int
		var udplRtp *streamUdpListener
		var udplRtcp *streamUdpListener
		func() {
			for {
				// choose two consecutive ports in range 65536-10000
				// rtp must be pair and rtcp odd
				rtpPort = (rand.Intn((65535-10000)/2) * 2) + 10000
				rtcpPort = rtpPort + 1

				var err error
				udplRtp, err = newStreamUdpListener(s.p, rtpPort)
				if err != nil {
					continue
				}

				udplRtcp, err = newStreamUdpListener(s.p, rtcpPort)
				if err != nil {
					udplRtp.close()
					continue
				}

				return
			}
		}()

		transportHeader := strings.Join([]string{
			"RTP/AVP/UDP",
			"unicast",
			fmt.Sprintf("client_port=%d-%d", rtpPort, rtcpPort),
		}, ";")

		if clientAnnounce {
			transportHeader += ";mode=record"
		}

		res, err := conn.WriteRequest(&gortsplib.Request{
			Method: gortsplib.SETUP,
			Url: func() *url.URL {
				control := media.Attributes.Value("control")

				// no control attribute
				if control == "" {
					return s.ur
				}

				// absolute path
				if strings.HasPrefix(control, "rtsp://") {
					ur, err := url.Parse(control)
					if err != nil {
						return s.ur
					}
					return ur
				}

				// relative path
				return &url.URL{
					Scheme: "rtsp",
					Host:   s.ur.Host,
					Path: func() string {
						ret := s.ur.Path

						if len(ret) == 0 || ret[len(ret)-1] != '/' {
							ret += "/"
						}

						control := media.Attributes.Value("control")
						if control != "" {
							ret += control
						} else {
							ret += "trackID=" + strconv.FormatInt(int64(i+1), 10)
						}

						return ret
					}(),
					RawQuery: s.ur.RawQuery,
				}
			}(),
			Header: gortsplib.Header{
				"Transport": []string{transportHeader},
			},
		})
		if err != nil {
			s.log("Setup ERR: %s", err)
			udplRtp.close()
			udplRtcp.close()
			return true
		}

		s.log("Sent SETUP to server, response %s", res.StatusMessage)

		if res.StatusCode != gortsplib.StatusOK {
			s.log("ERR: SETUP returned code %d (%s)", res.StatusCode, res.StatusMessage)
			udplRtp.close()
			udplRtcp.close()
			return true
		}

		tsRaw, ok := res.Header["Transport"]
		if !ok || len(tsRaw) != 1 {
			s.log("ERR: transport header not provided")
			udplRtp.close()
			udplRtcp.close()
			return true
		}

		th := gortsplib.ReadHeaderTransport(tsRaw[0])
		rtpServerPort, rtcpServerPort := th.GetPorts("server_port")
		if rtpServerPort == 0 {
			s.log("ERR: server ports not provided")
			udplRtp.close()
			udplRtcp.close()
			return true
		}

		udplRtp.publisherIp = publisherIp
		udplRtp.publisherPort = rtpServerPort
		udplRtp.trackId = i
		udplRtp.flow = _TRACK_FLOW_RTP
		udplRtp.path = s.path

		udplRtcp.publisherIp = publisherIp
		udplRtcp.publisherPort = rtcpServerPort
		udplRtcp.trackId = i
		udplRtcp.flow = _TRACK_FLOW_RTCP
		udplRtcp.path = s.path

		streamUdpListenerPairs = append(streamUdpListenerPairs, streamUdpListenerPair{
			udplRtp:  udplRtp,
			udplRtcp: udplRtcp,
		})
	}

	res, err := conn.WriteRequest(&gortsplib.Request{
		Method: gortsplib.PLAY,
		Url: &url.URL{
			Scheme:   "rtsp",
			Host:     s.ur.Host,
			Path:     s.ur.Path,
			RawQuery: s.ur.RawQuery,
		},
	})
	if err != nil {
		s.log("PLAY ERR: %s", err)
		return true
	}

	s.log("Sent PLAY to server, response %s", res.StatusMessage)

	if res.StatusCode != gortsplib.StatusOK {
		s.log("ERR: PLAY returned code %d (%s)", res.StatusCode, res.StatusMessage)
		return true
	}

	for _, pair := range streamUdpListenerPairs {
		pair.udplRtp.start()
		pair.udplRtcp.start()
	}

	tickerSendKeepalive := time.NewTicker(_KEEPALIVE_INTERVAL)
	defer tickerSendKeepalive.Stop()

	tickerCheckStream := time.NewTicker(_CHECK_STREAM_INTERVAL)
	defer tickerSendKeepalive.Stop()

	func() {
		s.p.tcpl.mutex.Lock()
		defer s.p.tcpl.mutex.Unlock()
		s.state = _STREAM_STATE_READY
	}()

	defer func() {
		s.p.tcpl.mutex.Lock()
		defer s.p.tcpl.mutex.Unlock()
		s.state = _STREAM_STATE_STARTING

		// disconnect all clients
		for c := range s.p.tcpl.clients {
			if c.path == s.path {
				c.close()
			}
		}
	}()

	s.log("ready")

	for {
		select {
		case <-s.terminate:
			return false

		case <-tickerSendKeepalive.C:
			_, err = conn.WriteRequest(&gortsplib.Request{
				Method: gortsplib.OPTIONS,
				Url: &url.URL{
					Scheme: "rtsp",
					Host:   s.ur.Host,
					Path:   "/",
				},
			})
			if err != nil {
				s.log("Options ERR: %s", err)
				return true
			}

		case <-tickerCheckStream.C:
			lastFrameTime := time.Time{}

			getLastFrameTime := func(l *streamUdpListener) {
				l.mutex.Lock()
				defer l.mutex.Unlock()
				if l.lastFrameTime.After(lastFrameTime) {
					lastFrameTime = l.lastFrameTime
				}
			}

			for _, pair := range streamUdpListenerPairs {
				getLastFrameTime(pair.udplRtp)
				getLastFrameTime(pair.udplRtcp)
			}

			if time.Since(lastFrameTime) >= _STREAM_DEAD_AFTER {
				s.log("ERR: stream is dead")
				return true
			}
		}
	}
}

func (s *stream) runTcp(conn *gortsplib.ConnClient, clientAnnounce bool) bool {
	for i, media := range s.clientSdpParsed.Medias {
		interleaved := fmt.Sprintf("interleaved=%d-%d", (i * 2), (i*2)+1)

		transportHeader := strings.Join([]string{
			"RTP/AVP/TCP",
			"unicast",
			interleaved,
		}, ";")

		if clientAnnounce {
			transportHeader += ";mode=record"
		}

		res, err := conn.WriteRequest(&gortsplib.Request{
			Method: gortsplib.SETUP,
			Url: func() *url.URL {
				control := media.Attributes.Value("control")

				// no control attribute
				if control == "" {
					return s.ur
				}

				// absolute path
				if strings.HasPrefix(control, "rtsp://") {
					ur, err := url.Parse(control)
					if err != nil {
						return s.ur
					}
					return ur
				}

				// relative path
				return &url.URL{
					Scheme: "rtsp",
					Host:   s.ur.Host,
					Path: func() string {
						ret := s.ur.Path

						if len(ret) == 0 || ret[len(ret)-1] != '/' {
							ret += "/"
						}

						control := media.Attributes.Value("control")
						if control != "" {
							ret += control
						} else {
							ret += "trackID=" + strconv.FormatInt(int64(i+1), 10)
						}

						return ret
					}(),
					RawQuery: s.ur.RawQuery,
				}
			}(),
			Header: gortsplib.Header{
				"Transport": []string{transportHeader},
			},
		})
		if err != nil {
			s.log("Setup ERR: %s", err)
			return true
		}

		if res.StatusCode != gortsplib.StatusOK {
			s.log("ERR: SETUP returned code %d (%s)", res.StatusCode, res.StatusMessage)
			return true
		}

		tsRaw, ok := res.Header["Transport"]
		if !ok || len(tsRaw) != 1 {
			s.log("ERR: transport header not provided")
			return true
		}

		th := gortsplib.ReadHeaderTransport(tsRaw[0])

		_, ok = th[interleaved]
		if !ok {
			s.log("ERR: transport header does not have %s (%s)", interleaved, tsRaw[0])
			return true
		}
	}

	err := conn.WriteRequestNoResponse(&gortsplib.Request{
		Method: gortsplib.PLAY,
		Url: &url.URL{
			Scheme:   "rtsp",
			Host:     s.ur.Host,
			Path:     s.ur.Path,
			RawQuery: s.ur.RawQuery,
		},
	})
	if err != nil {
		s.log("Write Request ERR: %s", err)
		return true
	}

outer:
	for {
		frame := &gortsplib.InterleavedFrame{
			Content: make([]byte, 512*1024),
		}
		vres, err := conn.ReadInterleavedFrameOrResponse(frame)
		if err != nil {
			s.log("ReadInterleavedFrameOrResponse ERR: %s", err)
			return true
		}

		switch res := vres.(type) {
		case *gortsplib.Response:
			if res.StatusCode != gortsplib.StatusOK {
				s.log("ERR: PLAY returned code %d (%s)", res.StatusCode, res.StatusMessage)
				return true
			}
			break outer

		case *gortsplib.InterleavedFrame:
			// ignore the frames sent before the response
		}
	}

	func() {
		s.p.tcpl.mutex.Lock()
		defer s.p.tcpl.mutex.Unlock()
		s.state = _STREAM_STATE_READY
	}()

	defer func() {
		s.p.tcpl.mutex.Lock()
		defer s.p.tcpl.mutex.Unlock()
		s.state = _STREAM_STATE_STARTING

		// disconnect all clients
		for c := range s.p.tcpl.clients {
			if c.path == s.path {
				c.close()
			}
		}
	}()

	s.log("ready")

	chanConnError := make(chan struct{})
	go func() {
		for {
			frame := &gortsplib.InterleavedFrame{
				Content: make([]byte, 512*1024),
			}
			err := conn.ReadInterleavedFrame(frame)
			if err != nil {
				s.log("ReadInterleaved ERR: %s", err)
				close(chanConnError)
				break
			}

			trackId, trackFlow := interleavedChannelToTrack(frame.Channel)

			func() {
				s.p.tcpl.mutex.RLock()
				defer s.p.tcpl.mutex.RUnlock()

				s.p.tcpl.forwardTrack(s.path, trackId, trackFlow, frame.Content)
			}()
		}
	}()

	select {
	case <-s.terminate:
		return false
	case <-chanConnError:
		return true
	}
}

func (s *stream) close() {
	if s.conn != nil {
		res, err := s.conn.WriteRequest(&gortsplib.Request{
			Method: gortsplib.TEARDOWN,
			Url: &url.URL{
				Scheme: "rtsp",
				Host:   s.ur.Host,
				Path:   "/",
			},
		})
		if err != nil {
			s.log("TEARDOWN ERR: %s", err)
		}

		s.log("Sent TEARDOWN to server, response %s", res.StatusMessage)
	}

	close(s.terminate)
	<-s.done
}
