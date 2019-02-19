package common

import (
	"crypto/rand"
	"fmt"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/appleboy/gin-jwt.v2"
)

// GetUserName returns the username based of the gin.Context
func GetUserName(c *gin.Context) string {
	// AuthUserKey is set by basic auth
	user, exists := c.Get(gin.AuthUserKey)
	if exists {
		return strings.ToLower(user.(string))
	}
	jwtClaims := jwt.ExtractClaims(c)
	return strings.ToLower(jwtClaims["id"].(string))
}

// GetUserMail returns the users mail address based of the gin.Context
func GetUserMail(c *gin.Context) string {
	jwtClaims := jwt.ExtractClaims(c)
	return jwtClaims["mail"].(string)
}

func RandomString(length int) string {
	key := make([]byte, length)
	_, err := rand.Read(key)
	if err != nil {
		log.Fatal(err)
	}
	return fmt.Sprintf("%x", key)
}

func ContainsEmptyString(ss ...string) bool {
	for _, s := range ss {
		if s == "" {
			return true
		}
	}
	return false
}

func RemoveDuplicates(elements []string) []string {
	encountered := map[string]bool{}

	// Create a map of all unique elements.
	for v := range elements {
		encountered[elements[v]] = true
	}

	// Place all keys from the map into a slice.
	result := []string{}
	for key := range encountered {
		result = append(result, key)
	}
	return result
}
