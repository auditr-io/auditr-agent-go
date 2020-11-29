package auth

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"
)

type rawToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

const (
	tokenURL    = "https://auditr.auth.us-west-2.amazoncognito.com/oauth2/token"
	userPoolURL = "https://cognito-idp.us-west-2.amazonaws.com/us-west-2_QQbPyOKoI"
)

var (
	timeFunc    = time.Now
	AccessToken string
)

func GetToken() {
	// if token != nil {
	// 	if token.ExpiresIn > now {
	// 		refreshToken()
	// 	}
	// }
}

func refreshToken() {
	token, err := parseToken(AccessToken)
	if err != nil {
		log.Println("parseToken(rawToken['access_token'].(string)) err: ", err)
		return
	}

	// tokenLifetime := time.Duration(rawToken["expires_in"].(int64)) * time.Second
	refreshBuffer := time.Duration(5) * time.Minute

	// TODO: if key not found, get from URL
	rawJwks := []byte(`{
		"keys": [
			{
				"alg": "RS256",
				"e": "AQAB",
				"kid": "RZ4lY980jJKQzvtdtzgAaAZkL152AK7lQz6UKGvEjoE=",
				"kty": "RSA",
				"n": "gH8Gu6_jOwPwBqQaORPt-vy2uaimugu6cC-vP57E4pW96eWrlft6SYXA9Hd-UGTy1rWNUyHH2nEnJvH_7muU4CzVaXA2k4Xuq-4PRO7a_sq0RUTjwGZ_ATD10YGc24PkOktKEcaojee1uGJ2zXfTYYwF-D6c56gzoCTyso2RL38L7-ezVpgK-30kmJszGTHLz0uNi8k5qeaKVgQ6_dEDB5thShIPh5CPdS6yGGzOEbd_Jlp3xcuQUAra4_7eFEcRd4hZlzut3izUL-eVeNKZ0YMDvmnGA6JK2zHDi16YnuvEx9ZJnfLmzouAuOuRPr6ANgxiudPrmcCj4zdglM0uSw",
				"use": "sig"
			},
			{
				"alg": "RS256",
				"e": "AQAB",
				"kid": "+uKN0jZNZQhU/ZptNHGpZXLCqmyfFss0ROzrBuYmiRE=",
				"kty": "RSA",
				"n": "3zjqBIC7WFFt_jEcpsl_DuryFhIfDj8yM2UWFP9V8zolCMNu79etwKVy_QbWkTBs11POQLpWw6J14yNoPKNim0XkdFc6hr3HyrRjSUTzDsAeaTixoM845mBKzlGLnKbnZd-BEQzT8l2J9ZCgm0D4CMqkFkFxp9OpZhE9OE-AKKczsJ9Y9l0tfBETfF4cuMr3zldxlfzKc5CdPAzuFKE-UG5ou5PJY6cxQ8HkSCjf0T7e1kfku9qrt7uUnpeE1WSDjfjAFrXHvdJHnP437dTY5M9jLiA0xr5pkjpA3z6jnvtNyXPxzGmUs4piGXYKi5Frlh_5DjslnJpxS-BV2or90w",
				"use": "sig"
			}
		]
	}`)

	var jwks *jose.JSONWebKeySet
	err = json.Unmarshal(rawJwks, &jwks)
	if err != nil {
		log.Println("json.Unmarshal(rawJwks, &jwks) err: ", err)
		return
	}

	claims := &jwt.Claims{}
	err = token.Claims(jwks, &claims)
	if err != nil {
		log.Println("token.Claims(jwks, &claims) err: ", err)
		return
	}

	expected := jwt.Expected{
		Issuer: userPoolURL,
	}
	err = claims.Validate(expected)
	if err != nil {
		log.Println("claims.Validate(expected) err: ", err)
		return
	}

	expiresAt := claims.Expiry.Time().Unix()
	refreshThreshold := expiresAt - int64(refreshBuffer.Seconds())
	if refresh(refreshThreshold) {
		fmt.Println("refresh")
	} else {
		fmt.Println("don't refresh")
	}
}

func requestToken() (*rawToken, error) {
	payload := strings.NewReader("grant_type=client_credentials&scope=/v1/events/write")

	client := &http.Client{} // TODO: make injectable
	req, err := http.NewRequest("POST", tokenURL, payload)
	if err != nil {
		log.Println("http.NewRequest err: ", err)
		return nil, err
	}

	// TODO: encode client id & secret from env
	req.Header.Add("Authorization", "Basic NTAzcWpnZnMxOWRuNjA5NzlldjEzcnFnbmI6MTg2MmgxZmJtY2QxNzRmcDRpMzA1cnVzNTd1cmU5NmY0cTM4OWpmMW52cmQ5MTEzNjE3cA==")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	res, err := client.Do(req)
	if err != nil {
		log.Println("client.Do(req) err: ", err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Println("ioutil.ReadAll(res.Body) err: ", err)
		return nil, err
	}

	rawToken := &rawToken{}
	err = json.Unmarshal(body, &rawToken)
	if err != nil {
		log.Println("json.Unmarshal(body) err: ", err)
		return nil, err
	}

	return rawToken, nil
}

func parseToken(accessToken string) (*jwt.JSONWebToken, error) {
	token, err := jwt.ParseSigned(accessToken)
	if err != nil {
		log.Println("jwt.Parse(accessToken err: ", err)
		return nil, err
	}

	return token, nil
}

func refresh(refreshThreshold int64) bool {
	now := timeFunc().Unix()
	return now >= refreshThreshold
}

func init() {
	rawToken, err := requestToken()
	if err != nil {
		log.Fatalln("requestToken() err: ", err)
		return
	}

	AccessToken = rawToken.AccessToken
}
