package openshift

import (
	"crypto/tls"
	"errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"

	"github.com/Jeffail/gabs/v2"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
)

func prometheusQueryHandler(c *gin.Context) {
	//params := c.Request.URL.Query()
	//clusterId := params.Get("clusterid")
	//query := params.Get("query")
	clusters := getOpenshiftClusters("")
	var bestCluster string
	for _, cluster := range clusters {
		values, err := prometheusQuery(cluster.ID, "up")
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: genericAPIError})
			return
		}
		if bestCluster == "" {
			bestCluster = cluster.ID
		}
		log.Printf("%v", values)
	}

	c.JSON(http.StatusBadRequest, common.ApiResponse{Message: bestCluster})
	// Already encoded as JSON
	//c.Data(http.StatusOK, gin.MIMEJSON, values.Bytes())
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

	prometheusRoute, err := gabs.ParseJSONBuffer(resp.Body)
	if err != nil {
		log.Println("error parsing body of response:", err)
		return nil, errors.New(genericAPIError)
	}

	prometheusHost := prometheusRoute.Path("spec.host").Data().(string)

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
