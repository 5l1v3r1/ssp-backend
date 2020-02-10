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
	"github.com/gin-gonic/gin"
)

func clusterCapacityHandler(c *gin.Context) {
	clusters := getOpenshiftClusters("")
	var bestCluster string
	var bestValue float64
	for _, cluster := range clusters {
		cpuRequests, err := singleValuePrometheusQuery(cluster.ID, "sum(kube_pod_container_resource_requests_cpu_cores and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(node:node_num_cpu:sum and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
			return
		}
		memRequests, err := singleValuePrometheusQuery(cluster.ID, "sum(kube_pod_container_resource_requests_memory_bytes and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(node:node_memory_bytes_total:sum and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
			return
		}
		podCapacity, err := singleValuePrometheusQuery(cluster.ID, "count(kube_pod_info and on(pod) kube_pod_container_status_running == 1 and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'}) / sum(kube_node_status_capacity_pods and on(node) kube_node_labels{label_node_role_kubernetes_io_compute='true'})")
		if err != nil {
			log.Printf("%v", err)
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
			return
		}
		value, ok := avgIsBetterThan(bestValue, cpuRequests, memRequests, podCapacity)
		if ok {
			bestValue = value
			bestCluster = cluster.ID
		}
		log.Printf("Cluster capacity %v: cpu: %v mem: %v pods: %v avg: %v", cluster.ID, cpuRequests, memRequests, podCapacity, value)
	}

	c.JSON(http.StatusBadRequest, common.ApiResponse{Message: bestCluster})
	// Already encoded as JSON
	//c.Data(http.StatusOK, gin.MIMEJSON, values.Bytes())
}

func avgIsBetterThan(bestValue float64, values ...float64) (float64, bool) {
	var total float64 = 0
	for _, v := range values {
		total += v
	}
	avg := total / float64(len(values))
	if avg < bestValue || bestValue == 0 {
		return avg, true
	}
	return avg, false
}

func singleValuePrometheusQuery(clusterId, query string) (float64, error) {
	res, err := prometheusQuery(clusterId, query)
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

func prometheusQuery(clusterId, query string) (*gabs.Container, error) {
	if clusterId == "" || query == "" {
		log.Printf("Missing clusterId or query parameter")
		return nil, errors.New(genericAPIError)
	}
	resp, err := getPrometheusHTTPClient("GET", clusterId, "api/v1/query?query="+url.QueryEscape(query), nil)
	if err != nil {
		log.Printf("Error getting Prometheus client: %v", err)
		return nil, errors.New(genericAPIError)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		//	    b, _ := ioutil.ReadAll(resp.Body)
		log.Printf("Error getting Prometheus http client (%v): %v", clusterId, resp.StatusCode)
		return nil, errors.New(genericAPIError)
	}
	body, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Printf("Error parsing Prometheus response: %v", err)
		return nil, errors.New(genericAPIError)
	}
	return body, nil
}

func getPrometheusHTTPClient(method, clusterId, apiPath string, body io.Reader) (*http.Response, error) {
	resp, err := getOseHTTPClient("GET", clusterId, "apis/route.openshift.io/v1/namespaces/openshift-monitoring/routes/prometheus-k8s", nil)
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
		log.Printf("Error getting Prometheus route (%v): %v", clusterId, json.Path("message").Data())
		return nil, errors.New(genericAPIError)
	}

	prometheusHost := json.Path("spec.host").Data().(string)

	cluster, err := getOpenshiftCluster(clusterId)
	if err != nil {
		return nil, err
	}

	token := cluster.Token
	if token == "" {
		log.Printf("WARNING: Cluster token not found. Please see README for more details. ClusterId: %v", clusterId)
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
