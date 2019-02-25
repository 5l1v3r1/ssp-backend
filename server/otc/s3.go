package otc

import (
	"encoding/json"
	"errors"
	//	"html"
	"log"
	"net/http"
	"regexp"
	"strings"

	//	"strconv"

	//	"fmt"

	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/common"
	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gin-gonic/gin"
)

const (
	genericAwsAPIError = "Fehler beim Aufruf der AWS API. Bitte erstelle ein Ticket"
)

const (
	s3CreateError = "Erstellung des Buckets fehlgeschlagen. Bitte erstelle ein Ticket"
	s3ListError   = "Die Buckets können nicht aufgelistet werden. Bitte erstelle ein Ticket"
)

const (
	accountProd    = "prod"
	accountNonProd = "nonprod"
)

const (
	stageDev  = "dev"
	stageTest = "test"
	stageInt  = "int"
	stageProd = "prod"
)

const (
	bucketReadPolicy  = "-BucketReadPolicy"
	bucketWritePolicy = "-BucketWritePolicy"
)

// PolicyDocument IAM Policy Document
type PolicyDocument struct {
	Version   string
	Statement []StatementEntry
}

// StatementEntry IAM Statement Entry
type StatementEntry struct {
	Effect   string
	Action   []string
	Resource []string
}

func validateNewS3Bucket(projectname string, bucketname string, billing string, stage string) error {
	if len(stage) == 0 {
		return errors.New("Umgebung muss definiert werden")
	}
	if len(billing) == 0 {
		return errors.New("Verrechnungsnummer muss definiert sein")
	}
	if len(bucketname) == 0 {
		return errors.New("Bucketname muss definiert sein")
	}
	if len(projectname) == 0 {
		return errors.New("Projekt muss definiert sein")
	}

	if len(bucketname) > 63 {
		// http://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html
		return errors.New("Generierter Bucketname " + bucketname + " ist zu lang")
	}
	var validName = regexp.MustCompile(`^[a-zA-Z0-9\-]+$`).MatchString
	if !validName(bucketname) {
		return errors.New("Bucketname kann nur alphanumerische Zeichen und Bindestriche enthalten")
	}

	svc, err := GetS3Client(stage)
	if err != nil {
		return err
	}

	result, err := svc.ListBuckets(nil)
	if err != nil {
		log.Print("Error while trying to validate new bucket (ListBucket call): " + err.Error())
		return errors.New(s3CreateError)
	}

	for _, b := range result.Buckets {
		if *b.Name == bucketname {
			log.Print("Error, bucket " + bucketname + "already exists")
			return errors.New("Fehler: Bucket " + bucketname + " existiert bereits")
		}
	}

	// Everything OK
	return nil
}

func listS3BucketsHandler(c *gin.Context) {
	username := common.GetUserName(c)

	log.Print(username + " lists S3 buckets")

	myBuckets, err := listS3BucketByUsername(username)
	if err != nil {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	} else {
		c.JSON(http.StatusOK, myBuckets)
	}
}

func newS3BucketHandler(c *gin.Context) {
	//username := common.GetUserName(c)
	username := "e507889"

	var data common.NewS3BucketCommand
	if c.BindJSON(&data) == nil {
		newbucketname, err := generateS3Bucketname(data.BucketName, data.Stage)
		if err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		if err := validateNewS3Bucket(data.Project, newbucketname, data.Billing, data.Stage); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
			return
		}

		log.Print("Creating new bucket " + newbucketname + " for " + username)

		if err := createNewS3Bucket(username, data.Project, newbucketname, data.Billing, data.Stage); err != nil {
			c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
		} else {
			c.JSON(http.StatusOK, common.ApiResponse{
				Message: "Es wurde ein neuer S3 Bucket erstellt: " + newbucketname +
					". Du kannst nun über den anderen Menüpunkt Benutzer für diesen Bucket erstellen",
			})
		}
	} else {
		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	}
}

func newS3UserHandler(c *gin.Context) {
	//	username := common.GetUserName(c)
	//	bucketName := c.Param("bucketname")
	//	cfg := config.Config()
	//
	//	var data common.NewS3UserCommand
	//	if c.BindJSON(&data) != nil {
	//		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: wrongAPIUsageError})
	//		return
	//	}
	//
	//	isNonProd := strings.HasSuffix(bucketName, accountNonProd)
	//	var stage string
	//	var loginURL string
	//	if isNonProd {
	//		stage = stageDev
	//		loginURL = cfg.GetString("aws_nonprod_login_url")
	//	} else {
	//		stage = stageProd
	//		loginURL = cfg.GetString("aws_prod_login_url")
	//	}
	//	//if err := validateNewS3User(username, bucketName, data.UserName, stage); err != nil {
	//	//	c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	//	//	return
	//	//}
	//
	//	log.Print(username + " creates a new user (" + data.UserName + ") for " + bucketName + " , readonly: " + strconv.FormatBool(data.IsReadonly))
	//
	//	credentials, err := createNewS3User(bucketName, data.UserName, stage, data.IsReadonly)
	//	if err != nil {
	//		c.JSON(http.StatusBadRequest, common.ApiResponse{Message: err.Error()})
	//		return
	//	}
	//	c.JSON(http.StatusOK, common.ApiResponse{
	//		Message: fmt.Sprintf("Der Benutzer (%v) wurde erstellt.<br><br><table>"+
	//			"<tr><td>Access Key ID:</td><td>%v</td></tr>"+
	//			"<tr><td>Secret Access Key:</td><td>%v</td></tr>"+
	//			"<tr><td>Password:</td><td>%v</td></tr>"+
	//			"<tr><td>Login URL:</td><td>%v</td></tr></table>"+
	//			"<br><b>Hinweis:</b> diese Keys und Passwörter gut & sicher abspeichern, da sie später nicht wiederhergestellt werden können!",
	//			credentials.Username, credentials.AccessKeyID, credentials.SecretKey, html.EscapeString(credentials.Password), loginURL)})
}

func createNewS3Bucket(username string, projectname string, bucketname string, billing string, stage string) error {
	svc, err := GetS3Client(stage)
	if err != nil {
		return err
	}

	_, err = svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketname),
	})
	if err != nil {
		log.Print("Error on CreateBucket call (username=" + username + ", bucketname=" + bucketname + "): " + err.Error())
		return errors.New(s3CreateError)
	}

	// Wait until bucket is created before finishing
	log.Print("Waiting for bucket " + bucketname + " to be created...")
	err = svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucketname),
	})

	if err != nil {
		log.Print("Error when creating S3 bucket in WaitUntilBucketExists: " + err.Error())
		return errors.New(s3CreateError)
	}

	_, err = svc.PutBucketTagging(&s3.PutBucketTaggingInput{
		Bucket: aws.String(bucketname),
		Tagging: &s3.Tagging{
			TagSet: []*s3.Tag{
				{Key: aws.String("Creator"), Value: aws.String(username)},
				{Key: aws.String("Project"), Value: aws.String(projectname)},
				{Key: aws.String("Accounting_Number"), Value: aws.String(billing)},
				{Key: aws.String("Stage"), Value: aws.String(stage)},
			},
		}})
	if err != nil {
		log.Print("Tagging bucket " + bucketname + " failed: " + err.Error())
		return errors.New(s3CreateError)
	}

	log.Print("Creating IAM policies for bucket " + bucketname + "...")

	// Create a IAM service client.
	iamSvc, err := GetIAMClient(stage)
	if err != nil {
		return err
	}

	readPolicy := PolicyDocument{
		Version: "2012-10-17",
		Statement: []StatementEntry{
			{
				Effect: "Allow",
				Action: []string{
					"s3:Get*",  // Allow Get commands
					"s3:List*", // Allow List commands
				},
				Resource: []string{
					"arn:aws:s3:::" + bucketname,
					"arn:aws:s3:::" + bucketname + "/*",
				},
			},
		},
	}

	writePolicy := PolicyDocument{
		Version: "2012-10-17",
		Statement: []StatementEntry{
			{
				Effect: "Allow",
				Action: []string{
					"s3:Get*",    // Allow Get commands
					"s3:List*",   // Allow List commands
					"s3:Put*",    // Allow Put commands
					"s3:Delete*", // Allow Delete commands
				},
				Resource: []string{
					"arn:aws:s3:::" + bucketname,
					"arn:aws:s3:::" + bucketname + "/*",
				},
			},
		},
	}

	// Read policy
	b, err := json.Marshal(&readPolicy)
	if err != nil {
		log.Print("Error marshaling readPolicy: " + err.Error())
		return errors.New(s3CreateError)
	}

	_, err = iamSvc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(b)),
		PolicyName:     aws.String(bucketname + bucketReadPolicy),
	})
	if err != nil {
		log.Print("Error CreatePolicy for BucketReadPolicy failed: " + err.Error())
		return errors.New(s3CreateError)
	}

	// Write policy
	c, err := json.Marshal(&writePolicy)
	if err != nil {
		log.Print("Error marshaling writePolicy: " + err.Error())
		return errors.New(s3CreateError)
	}

	_, err = iamSvc.CreatePolicy(&iam.CreatePolicyInput{
		PolicyDocument: aws.String(string(c)),
		PolicyName:     aws.String(bucketname + bucketWritePolicy),
	})
	if err != nil {
		log.Print("Error CreatePolicy for BucketWritePolicy failed: " + err.Error())
		return errors.New(s3CreateError)
	}

	log.Print("Bucket " + bucketname + " and IAM policies successfully created")

	return nil
}

func generateS3Bucketname(bucketname string, stage string) (string, error) {
	// Generate bucketname: <prefix>-<bucketname>-<stage_suffix>
	bucketPrefix := config.Config().GetString("aws_s3_bucket_prefix")

	account, err := getAccountForStage(stage)
	if err != nil {
		return "", err
	}

	return strings.ToLower(bucketPrefix + "-" + bucketname + "-" + account), nil
}

func listS3BucketByUsername(username string) (*common.BucketListResponse, error) {
	result := common.BucketListResponse{
		Buckets: []common.Bucket{},
	}
	nonProdBuckets, err := listS3BucketByUsernameForAccount(username, accountNonProd)
	if err != nil {
		return nil, err
	}
	prodBuckets, err := listS3BucketByUsernameForAccount(username, accountProd)
	if err != nil {
		return nil, err
	}

	result.Buckets = append(result.Buckets, nonProdBuckets...)
	result.Buckets = append(result.Buckets, prodBuckets...)

	return &result, nil
}

func listS3BucketByUsernameForAccount(username string, account string) ([]common.Bucket, error) {
	var stage string
	if account == accountProd {
		stage = stageProd
	} else {
		stage = stageDev
	}

	svc, err := GetS3Client(stage)
	if err != nil {
		return nil, err
	}

	result, err := svc.ListBuckets(nil)
	if err != nil {
		log.Print("Unable to list buckets (ListBuckets API call): " + err.Error())
		return nil, errors.New(s3ListError)
	}

	buckets := []common.Bucket{}
	for _, b := range result.Buckets {
		// Get bucket tags
		taggingParams := &s3.GetBucketTaggingInput{
			Bucket: aws.String(*b.Name),
		}
		result, err := svc.GetBucketTagging(taggingParams)
		if err != nil {
			log.Print("Unable to get tags for bucket " + *b.Name + ", username " + username + ": " + err.Error())
			// Something went wrong with this bucket (probably no tags). Don't fail, just skip this bucket
			continue
		}

		// Get list of buckets where "Creator" equals username and return only those
		for _, tag := range result.TagSet {
			if *tag.Key == "Creator" && strings.ToLower(*tag.Value) == strings.ToLower(username) {
				buckets = append(buckets, common.Bucket{Name: *b.Name, Account: account})
			}
		}
	}
	return buckets, nil
}

func getAwsSession(account string) (*session.Session, error) {
	cfg := config.Config()
	// Validate necessary env variables
	region := cfg.GetString("otc_region")
	if region == "" {
		log.Println("WARNING: Env variable 'OTC_REGION' must be specified")
		return nil, errors.New(common.ConfigNotSetError)
	}
	bucketPrefix := cfg.GetString("otc_s3_bucket_prefix")
	if bucketPrefix == "" {
		log.Println("WARNING: Env variable 'OTC_S3_BUCKET_PREFIX' must be specified")
		return nil, errors.New(common.ConfigNotSetError)
	}

	// Create AWS session based on account
	var accessKeyID string
	var accessSecret string

	switch account {
	case accountProd:
		accessKeyID = cfg.GetString("otc_nonprod_access_key_id")
		accessSecret = cfg.GetString("otc_nonprod_secret_access_key")
	case accountNonProd:
		accessKeyID = cfg.GetString("otc_nonprod_access_key_id")
		accessSecret = cfg.GetString("otc_nonprod_secret_access_key")
	default:
		log.Println("Invalid account: " + account)
	}

	endpoint := "https://obs.eu-ch.o13bb.otc.t-systems.com"
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(accessKeyID, accessSecret, ""),
		Region:      aws.String(region),
		Endpoint:    &endpoint,
	})

	if err != nil {
		log.Println("Error creating aws session: ", err.Error())
		return nil, errors.New(genericAwsAPIError)
	}

	return sess, nil
}

// getAccountForStage remapps the stage string form the UI to
// the technical AWS account
// dev, test, int = NONPROD
// prod = PROD
func getAccountForStage(stage string) (string, error) {
	switch stage {
	case stageDev, stageTest, stageInt:
		return accountNonProd, nil
	case stageProd:
		return accountProd, nil
	default:
		log.Println("Could not map to account, invalid stage: " + stage)
		return "", errors.New(wrongAPIUsageError)
	}
}

func GetS3Client(stage string) (*s3.S3, error) {
	account, err := getAccountForStage(stage)
	if err != nil {
		return nil, err
	}

	sess, err := getAwsSession(account)
	if err != nil {
		return nil, err
	}
	return s3.New(sess), nil
}

func GetIAMClient(stage string) (*iam.IAM, error) {
	account, err := getAccountForStage(stage)
	if err != nil {
		return nil, err
	}

	sess, err := getAwsSession(account)
	if err != nil {
		return nil, err
	}
	return iam.New(sess), nil
}
