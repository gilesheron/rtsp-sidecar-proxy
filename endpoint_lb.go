package main

import (
	"net"
	"hash/fnv"
)

// LoadBalancer -> Define general load-balancer interface for
// specific algorithms to implement.
type LoadBalancer interface {
	getMapping(net.IP) (net.IP, error)
	setEndpoints([]net.IP)
}

// MapToEndpoint -> Public function which gets the mapping from
// input ip to endpoint ip
func MapToEndpoint(lb LoadBalancer, ip net.IP) (net.IP, error) {
	return lb.getMapping(ip)
}

func AddTestingEndpoints(lb LoadBalancer, endpoints []net.IP) {
	lb.setEndpoints(endpoints)
}

// getSvcEndpoints -> Given a cluster ip, retrieve the endpoints
// associated.
func getSvcEndpoints(clusterIP net.IP) ([]net.IP, error) {
	return nil, nil
}

type HashModLB struct {
	endpoints []net.IP
}

func NewHashModLB() *HashModLB {
	return &HashModLB{endpoints: nil}
}

func (hm *HashModLB) setEndpoints(endpoints []net.IP) {
	hm.endpoints = endpoints
}

// getMapping -> Hash the input ip then mod it by the number of
// indices we have.
func (hm *HashModLB) getMapping(ip net.IP) (net.IP, error) {
	ipHash, err := hash(ip.String())

	if err != nil {
		return nil, err
	}
	var index = ipHash % uint32(len(hm.endpoints))

	var mapping = hm.endpoints[index]
	return  mapping, nil
}

func hash(s string) (uint32, error) {
	h := fnv.New32a()
	_, err := h.Write([]byte(s))

	if err != nil {
		return 0, err
	}
	return h.Sum32(), nil
}

