package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
)

// LoadBalancer -> Define general load-balancer interface for
// specific algorithms to implement.
type LoadBalancer interface {
	getMapping(string) (string, error)
	setResources([]string)
}

// MapToEndpoint -> Public function which gets the mapping from
// input ip to endpoint ip
func MapToUrl(lb LoadBalancer, ip string) (string, error) {
	return lb.getMapping(ip)
}

func UseTestingEndpoints(lb LoadBalancer, resources []string) {
	lb.setResources(resources)
}

// getSvcUrls -> Given a dest ip, retrieve the associated endpoints
func getUrls(resource string) ([]string, error) {
	host := "k8s-apiclient-service:9899"
	path := "/svc/resources"
	queryParams := "resource=" + url.QueryEscape(resource)

	query := fmt.Sprintf("http://%s%s?%s", host, path, queryParams)

	log.Printf("Query = %s", query)

	resp, err := http.Get(query)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		return nil, err
	}

	var resources []string
	err = json.Unmarshal(body, &resources)
	if err != nil {
		return nil, err
	} else if len(resources) == 0 {
		return nil, fmt.Errorf("No resources for url %s!", resource)
	}

	return resources, nil
}

type RoundRobinLB struct {
	resources []string
	index     int
}

func (rr *RoundRobinLB) setResources(urls []string) {
	rr.resources = urls
}

func NewRoundRobinLB(resource string) (*RoundRobinLB, error) {
	LB := RoundRobinLB{}
	resources, err := getUrls(resource)

	if err != nil {
		return &LB, err
	}

	LB.resources = resources
	LB.index = -1
	return &LB, nil
}

// getMapping -> return endpoint at index then update index
func (rr *RoundRobinLB) getMapping(resource string) (string, error) {
	err := rr.advanceIndex()

	if err != nil {
		return "", err
	}

	value := rr.resources[rr.index]
	return value, nil
}

func (rr *RoundRobinLB) advanceIndex() error {

	if len(rr.resources) == 0 {
		return fmt.Errorf("len of endpoints cant be 0")
	}

	// edge case, index needs to reset to 0
	if rr.index == (len(rr.resources) - 1) {
		rr.index = 0
	} else {
		rr.index += 1
	}

	return nil
}

// HashModLB -> Simple hash modulo load balancer type
type HashModLB struct {
	resources []string
}

// NewHashModLB -> Instantiate load balander
func NewHashModLB(clusterIP string) (*HashModLB, error) {
	LB := HashModLB{}
	endpoints, err := getUrls(clusterIP)

	if err != nil {
		return &LB, err
	}

	LB.resources = endpoints
	return &LB, nil
}

func (hm *HashModLB) setEndpoints(endpoints []string) {
	hm.resources = endpoints
}

// getMapping -> Hash the input ip then mod it by the number of
// indices we have.
func (hm *HashModLB) getMapping(clientIP string) (string, error) {
	ipHash, err := hash(clientIP)

	if err != nil {
		return "", err
	}

	var index = ipHash % uint32(len(hm.resources))
	var mapping = hm.resources[index]
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

