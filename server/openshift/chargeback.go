package openshift

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/gin-gonic/gin"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type OpenshiftChargebackCommand struct {
	Start           time.Time
	End             time.Time
	Cluster         Cluster
	ProjectContains string
}

type ApiResponse struct {
	CSV  string       `json:"csv"`
	Rows []Ressources `json:"rows"`
}

// JSON Types
type Quota struct {
	Facets []QuotaFacet
}

type QuotaFacet struct {
	Name    string
	Results []QuotaResult
}

type QuotaResult struct {
	Average float64
}

type Usage struct {
	Facets []UsageFacet
}

type UsageFacet struct {
	Name    string
	Results []UsageResult
}

type UsageResult struct {
	Result float64
}

type Assignment struct {
	Facets []AssignmentFacet
}

type AssignmentFacet struct {
	Name    string
	Results []AssignmentResult
}

type AssignmentResult struct {
	Latest string
}

// Internal types
type Ressources struct {
	Project             string
	ReceptionAssignment string
	OrderReception      string
	PspElement          string
	UsedCpu             float64
	UsedMemory          float64
	QuotaCpu            float64
	QuotaMemory         float64
	RequestedCpu        float64
	RequestedMemory     float64
	Storage             float64
	Prices              Pricing
}

type Pricing struct {
	QuotaCpu        float64
	QuotaMemory     float64
	Storage         float64
	RequestedCpu    float64
	RequestedMemory float64
	UsedCpu         float64
	UsedMemory      float64
}

type Queries struct {
	quotaQuery      string
	assignmentQuery string
	usageQueries    []string
}

type Cluster string

const (
	awsCluster  Cluster = "aws"
	viasCluster Cluster = "vias"
)

const requestUrl = "https://insights-api.newrelic.com/v1/accounts/1159282/query?nrql="
const viasSourceQuotaAssignment = "OpenshiftViasQuota"
const awsSourceQuotaAssignment = "OpenshiftAwsQuota"
const viasSourceUsage = "fullHostname like '%.sbb.ch'"
const awsSourceUsage = "`ec2Tag_Environment` = 'prod' OR hostname like 'node%'"
const sourceKey = "SOURCE"
const betweenKey = "BETWEEN"
const projectKey = "PROJECT_KEY"
const projectSearchKey = "PROJECT_SEARCH"
const projectQuotaSearchTemplate = "WHERE project like 'PROJECT_KEY'"
const projectUsageSearchTempate = "AND `containerLabel_io.kubernetes.pod.namespace` LIKE 'PROJECT_KEY'"

const dateFormat = "2006-01-02 15:04:05"

// Templates
const quotaQueryTemplate = "SELECT average(cpuHard) AS CpuQuota, average(cpuUsed) AS CpuRequests, average(memoryHard) AS MemoryQuota, average(memoryUsed) AS MemoryRequests, average(storage) AS Storage " +
	"FROM SOURCE FACET project PROJECT_SEARCH BETWEEN LIMIT 1000"

const usageQueryTemplate = "SELECT rate(sum(cpuPercent), 60 minutes)/100 as CPU, rate(sum(memoryResidentSizeBytes), 60 minutes)/(1000*1000*1000) as GB " +
	"FROM ProcessSample FACET `containerLabel_io.kubernetes.pod.namespace` WHERE SOURCE PROJECT_SEARCH BETWEEN LIMIT 1000"

const assignmentQueryTemplate = "SELECT latest(accountAssignment), latest(megaId) FROM SOURCE FACET project PROJECT_SEARCH LIMIT 1000"

func chargebackHandler(c *gin.Context) {
	username := common.GetUserName(c)
	var unitprices = Pricing{
		QuotaCpu:        10.0,
		QuotaMemory:     2.5,
		RequestedCpu:    40.0,
		RequestedMemory: 10,
		UsedCpu:         40,
		UsedMemory:      10,
		Storage:         1.0,
	}
	const managementFee = 1.0625

	fmt.Printf("%v called openshift chargeback\n", username)
	var data OpenshiftChargebackCommand
	if err := c.BindJSON(&data); err == nil {
		start := data.Start
		end := data.End
		var cluster Cluster = data.Cluster
		projectContains := data.ProjectContains

		// Programm
		var resourceMap = make(map[string]Ressources)

		client := &http.Client{}

		quota := new(Quota)
		usage := new(Usage)
		assignment := new(Assignment)

		queries := computeQueries(start, end, projectContains, cluster)

		fmt.Println(queries.assignmentQuery)
		/*	  fmt.Println(queries.quotaQuery)
			  fmt.Println(queries.usageQueries)*/

		getJson(client, queries.quotaQuery, quota)
		addQuotaAndRequestedToResources(resourceMap, quota)

		getJson(client, queries.assignmentQuery, assignment)
		addAssignmentToResources(resourceMap, assignment)

		for i := range queries.usageQueries {
			getJson(client, queries.usageQueries[i], usage)
			addUsedToResources(resourceMap, usage)
		}
		normalizedResourceUsage(resourceMap, float64(len(queries.usageQueries)))
		computeResourcePrices(resourceMap, unitprices, managementFee)

		report := createCSVReport(resourceMap)

		v := make([]Ressources, 0, len(resourceMap))
		for _, value := range resourceMap {
			v = append(v, value)
		}
		c.JSON(http.StatusOK, ApiResponse{
			CSV:  report,
			Rows: v,
		})
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Wrong API usage"})
		fmt.Println(err.Error())
	}
}

func computeQueries(start time.Time, end time.Time, searchString string, cluster Cluster) Queries {
	sourceQuotaAssignment := viasSourceQuotaAssignment
	sourceUsage := viasSourceUsage

	if cluster == awsCluster {
		sourceQuotaAssignment = awsSourceQuotaAssignment
		sourceUsage = awsSourceUsage
	}

	projectQuotaSearch := ""
	projectUsageSearch := ""

	if searchString != "" {
		projectQuotaSearch = strings.Replace(projectQuotaSearchTemplate, projectKey, searchString, len(projectQuotaSearchTemplate))
		projectUsageSearch = strings.Replace(projectUsageSearchTempate, projectKey, searchString, len(projectQuotaSearchTemplate))
	}

	quotaQuery := strings.Replace(quotaQueryTemplate, sourceKey, sourceQuotaAssignment, len(quotaQueryTemplate))
	between := fmt.Sprintf("SINCE '%s'", start.Format(dateFormat))
	between += fmt.Sprintf(" UNTIL '%s'", end.Format(dateFormat))
	quotaQuery = strings.Replace(quotaQuery, betweenKey, between, len(quotaQuery))
	quotaQuery = strings.Replace(quotaQuery, projectSearchKey, projectQuotaSearch, len(quotaQuery))

	assignmentQuery := strings.Replace(assignmentQueryTemplate, sourceKey, sourceQuotaAssignment, len(assignmentQueryTemplate))
	assignmentQuery = strings.Replace(assignmentQuery, projectSearchKey, projectQuotaSearch, len(assignmentQuery))

	usageQueries := make([]string, 0)
	usageQuery := strings.Replace(usageQueryTemplate, sourceKey, sourceUsage, len(usageQueryTemplate))
	usageQuery = strings.Replace(usageQuery, projectSearchKey, projectUsageSearch, len(usageQuery))

	duration, _ := time.ParseDuration("240h")
	current := start

	for current.Before(end) {
		clause := fmt.Sprintf("SINCE '%s'", current.Format(dateFormat))
		current = current.Add(duration)
		if current.Before(end) {
			clause += fmt.Sprintf(" UNTIL '%s'", current.Format(dateFormat))
		} else {
			clause += fmt.Sprintf(" UNTIL '%s'", end.Format(dateFormat))
		}
		usageQueries = append(usageQueries, strings.Replace(usageQuery, betweenKey, clause, len(usageQuery)))
	}
	return Queries{quotaQuery: quotaQuery, assignmentQuery: assignmentQuery, usageQueries: usageQueries}
}

func getJson(client *http.Client, query string, target interface{}) error {

	var url = requestUrl + url.QueryEscape(query)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println(err)
	}

	newrelic_api_token := os.Getenv("NEWRELIC_API_TOKEN")
	if len(newrelic_api_token) == 0 {
		log.Fatal("Env variable 'NEWRELIC_API_TOKEN' must be specified")
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("X-Query-Key", newrelic_api_token)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(target)
}

func addQuotaAndRequestedToResources(resourceMap map[string]Ressources, quota *Quota) {
	for i := range quota.Facets {
		facet := quota.Facets[i]
		project := facet.Name
		quotaCpu := facet.Results[0].Average
		requestCpu := facet.Results[1].Average
		quotaMemory := facet.Results[2].Average
		requestMemory := facet.Results[3].Average
		storage := facet.Results[4].Average

		if val, ok := resourceMap[project]; ok {
			val.QuotaCpu = quotaCpu
			val.RequestedCpu = requestCpu
			val.QuotaMemory = quotaMemory
			val.RequestedMemory = requestMemory
			val.Storage = storage
			resourceMap[project] = val
		} else {
			resourceMap[project] = Ressources{
				Project:         project,
				QuotaCpu:        quotaCpu,
				RequestedCpu:    requestCpu,
				QuotaMemory:     quotaMemory,
				RequestedMemory: requestMemory,
				Storage:         storage,
			}
		}
	}
}

func addUsedToResources(resourceMap map[string]Ressources, used *Usage) {
	for i := range used.Facets {
		facet := used.Facets[i]
		project := facet.Name
		usedCpu := facet.Results[0].Result
		usedMemory := facet.Results[1].Result
		if val, ok := resourceMap[project]; ok {
			val.UsedCpu = usedCpu + val.UsedCpu
			val.UsedMemory = usedMemory + val.UsedMemory
			resourceMap[project] = val
		} else {
			resourceMap[project] = Ressources{
				Project:    project,
				UsedCpu:    usedCpu,
				UsedMemory: usedMemory,
			}
		}
	}
}

func addAssignmentToResources(resourceMap map[string]Ressources, assignment *Assignment) {

	for i := range assignment.Facets {
		receptionAssignment := ""
		orderReception := ""
		pspElement := ""

		facet := assignment.Facets[i]
		project := facet.Name
		accountAssignment := facet.Results[0].Latest
		if strings.HasPrefix(accountAssignment, "77") {
			receptionAssignment = accountAssignment
		} else if strings.HasPrefix(accountAssignment, "70") {
			orderReception = accountAssignment
		} else {
			pspElement = accountAssignment
		}

		if val, ok := resourceMap[project]; ok {
			val.ReceptionAssignment = receptionAssignment
			val.OrderReception = orderReception
			val.PspElement = pspElement
			resourceMap[project] = val
		} else {
			resourceMap[project] = Ressources{
				Project:             project,
				ReceptionAssignment: receptionAssignment,
				OrderReception:      orderReception,
				PspElement:          pspElement,
			}
		}
	}
}

func normalizedResourceUsage(resourceMap map[string]Ressources, count float64) {
	for key, value := range resourceMap {
		value.UsedCpu = value.UsedCpu / count
		value.UsedMemory = value.UsedMemory / count
		resourceMap[key] = value
	}
}

func computeResourcePrices(resourceMap map[string]Ressources, unitprices Pricing, management float64) {

	// preise per Tag und als übergabe noch die anzahl Tage angeben.
	for key, value := range resourceMap {
		quotaCpu := math.Ceil(value.QuotaCpu * unitprices.QuotaCpu * management)
		quotaMemory := math.Ceil(value.QuotaMemory * unitprices.QuotaMemory * management)
		requestedCpu := math.Ceil(value.RequestedCpu * unitprices.RequestedCpu * management)
		requestedMemory := math.Ceil(value.RequestedMemory * unitprices.RequestedMemory * management)
		usedCpu := math.Ceil(value.UsedCpu * unitprices.UsedCpu * management)
		usedMemory := math.Ceil(value.UsedMemory * unitprices.UsedMemory * management)
		storage := math.Ceil(value.Storage * unitprices.Storage * management)
		value.Prices = Pricing{
			QuotaCpu:        quotaCpu,
			QuotaMemory:     quotaMemory,
			RequestedCpu:    requestedCpu,
			RequestedMemory: requestedMemory,
			UsedCpu:         usedCpu,
			UsedMemory:      usedMemory,
			Storage:         storage,
		}
		resourceMap[key] = value
	}
}

func getConsolidatedPrice(value Ressources) string {
	s := value.Prices.QuotaCpu + value.Prices.RequestedCpu + value.Prices.QuotaMemory + value.Prices.RequestedMemory + value.Prices.UsedCpu + value.Prices.UsedMemory + value.Prices.Storage
	return strconv.FormatFloat(s, 'g', 6, 64)
}

func createCSVReport(resourceMap map[string]Ressources) string {
	const sender = "70029490"
	const art = "816753"
	const waehrung = "CHF"

	b := &bytes.Buffer{}
	wr := csv.NewWriter(b)
	//wr.Comma = ';'

	// Title row
	title := []string{"SendStelle", "SendAuftrag", "Sender-PSP-Element",
		"SendKdAuft", "SndPos", "SendNetzplan", "SendervorgangSVrg",
		"Kostenart", "Betrag", "Waehrung",
		"EmpfStelle", "EmpfAuftrag", "Empfaenger-PSP-Element",
		"EmpfKdAuft", "EmpPos", "EmpfNetzplan", "Evrg",
		"Menge gesamt", "ME", "PersNr", "Text", "Sys ID"}
	wr.Write(title)

	for key, value := range resourceMap {
		price := getConsolidatedPrice(value)

		row := []string{"", sender, "", "", "", "", "", art, price, waehrung,
			value.ReceptionAssignment, value.OrderReception, value.PspElement,
			"", "", "", "", "1", "ST", "", "LM1704 NCS " + key}

		wr.Write(row)
	}

	wr.Flush()

	return b.String()
}
