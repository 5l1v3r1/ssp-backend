package common

import (
	"log"
	"time"

	"github.com/SchweizerischeBundesbahnen/ssp-backend/server/config"
	"github.com/gin-gonic/gin"

	"github.com/jtblin/go-ldap-client"
	"gopkg.in/appleboy/gin-jwt.v2"
)

type login struct {
	Username string `form:"username" json:"username" binding:"required"`
	Password string `form:"password" json:"password" binding:"required"`
}

type User struct {
	UserId string
	Email  string
}

// GetAuthMiddleware returns a gin middleware for JWT with cookie based auth
func GetAuthMiddleware() *jwt.GinJWTMiddleware {
	key := config.Config().GetString("session_key")
	if key == "" {
		log.Fatal("Env variable 'SESSION_KEY' must be specified")
	}

	return &jwt.GinJWTMiddleware{
		Realm:         "CLOUD_SSP",
		Key:           []byte(key),
		Timeout:       time.Hour,
		MaxRefresh:    time.Hour,
		Authenticator: ldapAuthenticator,
		Authorizator: func(data interface{}, c *gin.Context) bool {
			return true
		},
		Unauthorized: func(c *gin.Context, code int, message string) {
			c.JSON(code, gin.H{
				"code":    code,
				"message": message,
			})
		},
		PayloadFunc: userPayloadFunc,
		TokenLookup: "header:Authorization",
		TimeFunc:    time.Now,
	}
}

func userPayloadFunc(data interface{}) jwt.MapClaims {
	if v, ok := data.(*User); ok {
		return jwt.MapClaims{
			"id":   v.UserId,
			"mail": v.Email,
		}
	}

	return jwt.MapClaims{}
}

func ldapAuthenticator(c *gin.Context) (interface{}, error) {
	cfg := config.Config()
	ldapHost := cfg.GetString("ldap_url")
	ldapBind := cfg.GetString("ldap_bind_dn")
	ldapBindPw := cfg.GetString("ldap_bind_cred")
	ldapFilter := cfg.GetString("ldap_filter")
	ldapSearchBase := cfg.GetString("ldap_search_base")

	client := &ldap.LDAPClient{
		Attributes:   []string{"givenName", "sn", "mail", "uid"},
		Base:         ldapSearchBase,
		Host:         ldapHost,
		Port:         389,
		UseSSL:       false,
		SkipTLS:      true,
		BindDN:       ldapBind,
		BindPassword: ldapBindPw,
		UserFilter:   ldapFilter,
	}

	// It is the responsibility of the caller to close the connection
	defer client.Close()

	var loginVals login
	if err := c.ShouldBind(&loginVals); err != nil {
		return "", jwt.ErrMissingLoginValues
	}
	userID := loginVals.Username
	password := loginVals.Password

	ok, user, err := client.Authenticate(userID, password)
	if err != nil {
		log.Printf("Error authenticating user %s: %+v", userID, err)
		return nil, jwt.ErrFailedAuthentication
	}
	if !ok {
		log.Printf("Authenticating failed for user %s", userID)
		return nil, jwt.ErrFailedAuthentication
	}

	return &User{
		UserId: userID,
		Email:  user["mail"],
	}, nil
}
