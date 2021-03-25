package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"net/http"
	"net/url"
)

// LoadBalancer -> Define general load-balancer interface for
// specific algorithms to implement.
type LoadBalancer interface {
	getMapping(string) (string, error)
	setEndpoints([]string)
}

// MapToEndpoint -> Public function which gets the mapping from
// input ip to endpoint ip
func MapToEndpoint(lb LoadBalancer, ip string) (string, error) {
	return lb.getMapping(ip)
}

func UseTestingEndpoints(lb LoadBalancer, endpoints []string) {
	lb.setEndpoints(endpoints)
}

// getSvcEndpoints -> Given a cluster ip, retrieve the endpoints
// associated.
func getSvcEndpoints(clusterIP string) ([]string, error) {
	host := "endpoint-api"
	path := "/endpoints"
	queryParams := "clusterIP=" + url.QueryEscape(clusterIP)

	query := fmt.Sprintf("%s%s?%s", host, path, queryParams)
	resp, err := http.Get(query)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	var endpoints []string
	err = json.Unmarshal(body, &endpoints)
	return endpoints, err
}

type RoundRobinLB struct {
	endpoints []string
	index     int
}

func (rr *RoundRobinLB) setEndpoints(endpoints []string) {
	rr.endpoints = endpoints
}

func NewRoundRobinLB(clusterIP string) (*RoundRobinLB, error) {
	LB := RoundRobinLB{}
	endpoints, err := getSvcEndpoints(clusterIP)

	if err != nil {
		return &LB, err
	}

	LB.endpoints = endpoints
	LB.index = -1
	return &LB, nil
}

// getMapping -> return endpoint at index then update index
func (rr *RoundRobinLB) getMapping(clusterIP string) (string, error) {
	err := rr.advanceIndex()

	if err != nil {
		return "", err
	}

	endpoint := rr.endpoints[rr.index]
	return endpoint, nil
}

func (rr *RoundRobinLB) advanceIndex() error {

	if len(rr.endpoints) == 0 {
		return fmt.Errorf("len of endpoints cant be 0")
	}

	// edge case, index needs to reset to 0
	if rr.index == len(rr.endpoints) {
		rr.index = 0
	} else {
		rr.index += 1
	}

	return nil
}

// HashModLB -> Simple hash modulo load balancer type
type HashModLB struct {
	endpoints []string
}

// NewHashModLB -> Instantiate load balander
func NewHashModLB(clusterIP string) (*HashModLB, error) {
	LB := HashModLB{}
	endpoints, err := getSvcEndpoints(clusterIP)

	if err != nil {
		return &LB, err
	}

	LB.endpoints = endpoints
	return &LB, nil
}

func (hm *HashModLB) setEndpoints(endpoints []string) {
	hm.endpoints = endpoints
}

// getMapping -> Hash the input ip then mod it by the number of
// indices we have.
func (hm *HashModLB) getMapping(clientIP string) (string, error) {
	ipHash, err := hash(clientIP)

	if err != nil {
		return "", err
	}

	var index = ipHash % uint32(len(hm.endpoints))
	var mapping = hm.endpoints[index]
	return mapping, nil
}

func hash(s string) (uint32, error) {
	h := fnv.New32a()
	_, err := h.Write([]byte(s))

	if err != nil {
		return 0, err
	}
	return h.Sum32(), nil
}
