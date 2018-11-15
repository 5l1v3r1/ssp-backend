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
	"text/template"
	"time"
)

type OpenshiftChargebackCommand struct {
	Start           time.Time
	End             time.Time
	Cluster         Cluster
	ProjectContains string
}

type ApiResponse struct {
	CSV  string      `json:"csv"`
	Rows []Resources `json:"rows"`
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
type Resources struct {
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

type templateValues struct {
	Source string
	Search string
	Since  string
	Until  string
}

type Cluster string

const (
	awsCluster  Cluster = "aws"
	viasCluster Cluster = "vias"
)

const viasSourceQuotaAssignment = "OpenshiftViasQuota"
const awsSourceQuotaAssignment = "OpenshiftAwsQuota"
const viasSourceUsage = "fullHostname like '%.sbb.ch'"
const awsSourceUsage = "`ec2Tag_Environment` = 'prod' OR hostname like 'node%'"

const dateFormat = "2006-01-02 15:04:05"

// Templates
const quotaQueryTemplate = "SELECT average(cpuHard) AS CpuQuota, average(cpuUsed) AS CpuRequests, average(memoryHard) AS MemoryQuota, average(memoryUsed) AS MemoryRequests, average(storage) AS Storage " +
	"FROM {{.Source}} FACET project WHERE project LIKE '{{.Search}}' SINCE '{{.Since}}' UNTIL '{{.Until}}' LIMIT 1000"

const usageQueryTemplate = "SELECT rate(sum(cpuPercent), 60 minutes)/100 as CPU, rate(sum(memoryResidentSizeBytes), 60 minutes)/(1000*1000*1000) as GB " +
	"FROM ProcessSample FACET `containerLabel_io.kubernetes.pod.namespace` WHERE {{.Source}} AND `containerLabel_io.kubernetes.pod.namespace` LIKE '{{.Search}}' SINCE '{{.Since}}' UNTIL '{{.Until}}' LIMIT 1000"

const assignmentQueryTemplate = "SELECT latest(accountAssignment), latest(megaId) FROM {{.Source}} FACET project WHERE project LIKE '{{.Search}}' LIMIT 1000"

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

	log.Printf("%v called openshift chargeback", username)
	var data OpenshiftChargebackCommand
	if err := c.BindJSON(&data); err != nil {
		fmt.Println(err.Error())
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: "Wrong API usage"})
		return
	}

	// Programm
	var resourceMap = make(map[string]Resources)

	client := &http.Client{}

	quota := new(Quota)
	usage := new(Usage)
	assignment := new(Assignment)

	queries := computeQueries(data.Start, data.End, data.ProjectContains, data.Cluster)

	/* fmt.Println(queries.assignmentQuery)
	fmt.Println(queries.quotaQuery)
	fmt.Println(queries.usageQueries) */

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

	report := createCSVReport(resourceMap, data.End)

	v := make([]Resources, 0, len(resourceMap))
	for _, value := range resourceMap {
		v = append(v, value)
	}
	c.JSON(http.StatusOK, ApiResponse{
		CSV:  report,
		Rows: v,
	})
}

func computeQueries(start time.Time, end time.Time, searchString string, cluster Cluster) Queries {
	sourceQuotaAssignment := viasSourceQuotaAssignment
	sourceUsage := viasSourceUsage

	if cluster == awsCluster {
		sourceQuotaAssignment = awsSourceQuotaAssignment
		sourceUsage = awsSourceUsage
	}

	s := templateValues{
		Source: sourceQuotaAssignment,
		Search: searchString,
		Since:  start.Format(dateFormat),
		Until:  end.Format(dateFormat),
	}

	var quotaQuery bytes.Buffer
	t, _ := template.New("quota").Parse(quotaQueryTemplate)
	t.Execute(&quotaQuery, s)

	var assignmentQuery bytes.Buffer
	t, _ = template.New("assignment").Parse(assignmentQueryTemplate)
	t.Execute(&assignmentQuery, s)

	usageQueries := make([]string, 0)
	t, _ = template.New("usage").Parse(usageQueryTemplate)
	duration, _ := time.ParseDuration("240h")
	current := start
	s.Source = sourceUsage
	for current.Before(end) {
		s.Since = current.Format(dateFormat)
		current = current.Add(duration)
		if current.Before(end) {
			s.Until = current.Format(dateFormat)
		} else {
			s.Until = end.Format(dateFormat)
		}
		var usageQuery bytes.Buffer
		t.Execute(&usageQuery, s)
		usageQueries = append(usageQueries, usageQuery.String())
	}
	return Queries{quotaQuery: quotaQuery.String(), assignmentQuery: assignmentQuery.String(), usageQueries: usageQueries}
}

func getJson(client *http.Client, query string, target interface{}) error {
	newrelic_api_token := os.Getenv("NEWRELIC_API_TOKEN")
	if len(newrelic_api_token) == 0 {
		log.Fatal("Env variable 'NEWRELIC_API_TOKEN' must be specified")
	}
	newrelic_api_account := os.Getenv("NEWRELIC_API_ACCOUNT")
	if len(newrelic_api_account) == 0 {
		log.Fatal("Env variable 'NEWRELIC_API_ACCOUNT' must be specified")
	}

	var url = fmt.Sprintf("https://insights-api.newrelic.com/v1/accounts/%v/query?nrql=%v", newrelic_api_account, url.QueryEscape(query))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println(err)
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

func addQuotaAndRequestedToResources(resourceMap map[string]Resources, quota *Quota) {
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
			resourceMap[project] = Resources{
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

func addUsedToResources(resourceMap map[string]Resources, used *Usage) {
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
			resourceMap[project] = Resources{
				Project:    project,
				UsedCpu:    usedCpu,
				UsedMemory: usedMemory,
			}
		}
	}
}

func addAssignmentToResources(resourceMap map[string]Resources, assignment *Assignment) {

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
			resourceMap[project] = Resources{
				Project:             project,
				ReceptionAssignment: receptionAssignment,
				OrderReception:      orderReception,
				PspElement:          pspElement,
			}
		}
	}
}

func normalizedResourceUsage(resourceMap map[string]Resources, count float64) {
	for key, value := range resourceMap {
		value.UsedCpu = value.UsedCpu / count
		value.UsedMemory = value.UsedMemory / count
		resourceMap[key] = value
	}
}

func computeResourcePrices(resourceMap map[string]Resources, unitprices Pricing, management float64) {

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

func getConsolidatedPrice(value Resources) string {
	s := value.Prices.QuotaCpu + value.Prices.RequestedCpu + value.Prices.QuotaMemory + value.Prices.RequestedMemory + value.Prices.UsedCpu + value.Prices.UsedMemory + value.Prices.Storage
	return strconv.FormatFloat(s, 'g', 6, 64)
}

func createCSVReport(resourceMap map[string]Resources, date time.Time) string {
	LMDateFormat := "0601"
	sender := os.Getenv("OPENSHIFT_CHARGEBACK_SENDER")
	if len(sender) == 0 {
		log.Print("Env variable 'OPENSHIFT_CHARGEBACK_SENDER' should be specified")
	}
	art := os.Getenv("OPENSHIFT_CHARGEBACK_ART")
	if len(art) == 0 {
		log.Print("Env variable 'OPENSHIFT_CHARGEBACK_ART' should be specified")
	}
	currency := os.Getenv("OPENSHIFT_CHARGEBACK_CURRENCY")
	if len(currency) == 0 {
		currency = "CHF"
	}

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

		row := []string{"", sender, "", "", "", "", "", art, price, currency,
			value.ReceptionAssignment, value.OrderReception, value.PspElement,
			"", "", "", "", "1", "ST", "", "LM" + date.Format(LMDateFormat) + " NCS " + key}

		wr.Write(row)
	}

	wr.Flush()

	return b.String()
}
