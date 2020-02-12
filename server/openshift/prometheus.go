package openshift

import (
	"crypto/tls"
	"errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
)

func setRecommendedCluster(clusters []OpenshiftCluster) error {
	var bestClusterIndex int
	var bestValue float64
	for i, cluster := range clusters {
		// skip private/deprecated clusters
		if cluster.Optgroup != "" {
			continue
		}
		cpuRequests, err := singleValuePrometheusQuery(cluster, "sum(kube_pod_container_resource_requests_cpu_cores and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(node:node_num_cpu:sum and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			return err
		}
		memRequests, err := singleValuePrometheusQuery(cluster, "sum(kube_pod_container_resource_requests_memory_bytes and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(node:node_memory_bytes_total:sum and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			return err
		}
		podCapacity, err := singleValuePrometheusQuery(cluster, "count(kube_pod_info and on(pod) kube_pod_container_status_running == 1 and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(kube_node_status_capacity_pods and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			return err
		}
		avg := weightedAverage(cpuRequests, memRequests, podCapacity)
		// Check if the weighted average is better than bestValue
		if avg < bestValue || bestValue == 0 {
			bestValue = avg
			bestClusterIndex = i
		}
		log.Printf("Cluster capacity %v: cpu: %v mem: %v pods: %v avg: %v", cluster.ID, cpuRequests, memRequests, podCapacity, avg)
	}
	clusters[bestClusterIndex].Recommended = true
	return nil
}

func weightedAverage(values ...float64) float64 {
	// Find the max value and index
	maxValue := values[0]
	maxIndex := 0
	for i, v := range values {
		if v > maxValue {
			maxValue = v
			maxIndex = i
		}
	}
	// Subtract the maximum percentage to achieve the remaining percentage
	maxComplement := 1 - maxValue
	// Sum the remaining values in the array (without the maxValue)
	var sumRemainingValues float64
	for i, v := range values {
		if i == maxIndex {
			continue
		}
		sumRemainingValues += v
	}
	// Use the sumRemainingValues to weight the remaining values
	total := maxValue * maxValue
	for i, v := range values {
		if i == maxIndex {
			continue
		}
		total += v * (v / sumRemainingValues * maxComplement)

	}
	return total
}

func singleValuePrometheusQuery(cluster OpenshiftCluster, query string) (float64, error) {
	res, err := prometheusQuery(cluster, query)
	if err != nil {
		return 0, err
	}
	if len(res.Path("data.result").Children()) > 1 {
		return 0, errors.New("Prometheus result contains more than one record")
	}
	// value is an array with the timestamp at index 0
	valueString := res.Path("data.result.0.value.1").Data().(string)
	value, err := strconv.ParseFloat(valueString, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func prometheusQuery(cluster OpenshiftCluster, query string) (*gabs.Container, error) {
	if query == "" {
		log.Printf("Missing query parameter")
		return nil, errors.New(genericAPIError)
	}
	resp, err := getPrometheusHTTPClient("GET", cluster, "api/v1/query?query="+url.QueryEscape(query), nil)
	if err != nil {
		log.Printf("Error getting Prometheus client: %v", err)
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		//	    b, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting Prometheus http client (%v): %v", cluster.ID, resp.StatusCode)
		return nil, errors.New(genericAPIError)
	}
	body, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Printf("Error parsing Prometheus response: %v", err)
		return nil, errors.New(genericAPIError)
	}
	return body, nil
}

func getPrometheusHTTPClient(method string, cluster OpenshiftCluster, apiPath string, body io.Reader) (*http.Response, error) {
	resp, err := getOseHTTPClient("GET", cluster.ID, "apis/route.openshift.io/v1/namespaces/openshift-monitoring/routes/prometheus-k8s", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	json, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Error getting Prometheus route (%v): %v", cluster.ID, json.Path("message").Data())
		return nil, errors.New(genericAPIError)
	}

	prometheusHost := json.Path("spec.host").Data().(string)

	token := cluster.Token
	if token == "" {
		log.Printf("WARNING: Cluster token not found. Please see README for more details. ClusterId: %v", cluster.ID)
		return nil, errors.New(common.ConfigNotSetError)
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest(method, "https://"+prometheusHost+"/"+apiPath, body)

	log.Debugf("Calling %v", req.URL.String())

	req.Header.Add("Authorization", "Bearer "+token)

	resp, err = client.Do(req)
	if err != nil {
		log.Println("Error from server: ", err.Error())
		return nil, errors.New(genericAPIError)
	}
	return resp, nil
}
