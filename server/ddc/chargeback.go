package ddc

import (
	"crypto/tls"
	log "github.com/sirupsen/logrus"
	"net/http"

	"encoding/csv"
	"errors"
	"strconv"
	"strings"

	"bytes"

	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"
	"time"
)

const apiErrorDDC = "Fehler beim Aufruf der DDC-API. Bitte erstelle ein Ticket bei DDC."

// RegisterRoutes registers the routes for OpenShift
func RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/ddc/billing", getDDCBillingHandler)
}

func getDDCBillingHandler(c *gin.Context) {
	username := common.GetUserName(c)
	log.Println("Called DDC Billing: ", username)

	rows, err := calculateDDCBilling()
	result := createCSVReport(rows)

	if err == nil {
		c.JSON(http.StatusOK, result)
	} else {
		c.JSON(http.StatusBadRequest, err.Error())
	}
}

func createCSVReport(rows []common.DDCBillingRow) common.DDCBilling {
	b := &bytes.Buffer{}
	wr := csv.NewWriter(b)
	wr.Comma = ';'

	// Title row
	title := []string{"SendStelle", "SendAuftrag", "Sender-PSP-Element",
		"SendKdAuft", "SndPos", "SendNetzplan", "SendervorgangSVrg",
		"Kostenart", "Betrag", "Waehrung",
		"EmpfStelle", "EmpfAuftrag", "Empfaenger-PSP-Element",
		"EmpfKdAuft", "EmpPos", "EmpfNetzplan", "Evrg",
		"Menge gesamt", "ME", "PersNr", "Text", "Sys ID"}
	wr.Write(title)

	for _, r := range rows {
		totalString := strconv.FormatFloat(r.Total, 'f', 2, 64)
		row := []string{"", r.Sender, "", "", "", "", "", r.Art, totalString, "CHF",
			r.ReceptionAssignment, r.OrderReception, r.PspElement,
			"", "", "", "", "1", "ST", "", r.Text, r.Project + ": " + r.Host}
		wr.Write(row)
	}

	wr.Flush()

	return common.DDCBilling{
		CSV:  b.String(),
		Rows: rows,
	}
}

func calculateDDCBilling() ([]common.DDCBillingRow, error) {
	client, req := getDDCClient()

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error calling ddc api: ", err.Error())
		return nil, errors.New(apiErrorDDC)
	} else {
		defer resp.Body.Close()
	}

	csvReader := csv.NewReader(resp.Body)
	csvReader.Comma = ';'

	records, err := csvReader.ReadAll()
	if err != nil {
		log.Println("Error parsing CSV.", err.Error())
		return nil, errors.New(apiErrorDDC)
	}

	const sender = "70029508"
	const art = "816750"
	const fee_server = 300.0
	const fee_client = 100.0
	const feeCpu = 30.0
	const feeMemory = 30.0
	const feeStorage = 1.0

	result := []common.DDCBillingRow{}

	// Text field contains YYMM; magic see https://medium.com/@Martynas/formatting-date-and-time-in-golang-5816112bf098
	text := "LM" + time.Now().Format("0601") + " DDC"

	for i, value := range records {
		if i > 0 {
			usedCpu, _ := strconv.ParseFloat(value[2], 64)
			usedMemory, _ := strconv.ParseFloat(value[3], 64)
			usedStorage, _ := strconv.ParseFloat(value[4], 64)

			var fee float64
			if strings.Contains(value[10], "Windows 7") {
				fee = fee_client
			} else {
				fee = fee_server
			}

			totalPrice := fee + usedCpu*feeCpu + usedMemory*feeMemory
			storagePrice := usedStorage * feeStorage

			hasBackup := value[11] == "Yes"
			if hasBackup {
				storagePrice = storagePrice * 2
			}

			totalPrice += storagePrice

			result = append(result, common.DDCBillingRow{
				Sender:              sender,
				Text:                text,
				Art:                 art,
				ReceptionAssignment: value[5],
				OrderReception:      value[6],
				PspElement:          value[7],
				Backup:              hasBackup,
				Total:               totalPrice,
				TotalCPU:            usedCpu * feeCpu,
				TotalMemory:         usedMemory * feeMemory,
				TotalStorage:        storagePrice,
				Project:             value[1],
				Host:                value[0],
			})
		}
	}

	return result, nil
}

func getDDCClient() (*http.Client, *http.Request) {
	api := config.Config().GetString("ddc_api")
	if api == "" {
		log.Fatal("Env variable 'DDC_API' must be specified")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	req, _ := http.NewRequest("GET", api, nil)

	log.Debugf("Calling %v", req.URL.String())

	return client, req
}
