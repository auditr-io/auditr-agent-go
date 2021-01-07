package lambda

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"testing"

	"github.com/auditr-io/auditr-agent-go/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockTransport is a mock Transport client
type mockTransport struct {
	mock.Mock
	http.Transport
	fn func(m *mockTransport, req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.fn != nil {
		return m.fn(m, req)
	}

	return m.successRoundTripResponse()
}

func (m *mockTransport) successRoundTripResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
	}, nil
}

type Response struct {
	Hello string `json:"hello"`
}

func createAgent() *Agent {
	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string   `json:"base_url"`
			EventsPath    string   `json:"events_path"`
			TargetRoutes  []string `json:"target"`
			SampledRoutes []string `json:"sampled"`
		}{
			BaseURL:       "https://dev-api.auditr.io/v1",
			EventsPath:    "/events",
			TargetRoutes:  []string{"POST /events", "PUT /events/:id"},
			SampledRoutes: []string{"GET /events", "GET /events/:id"},
		}
		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			}

			r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

			return &http.Response{
				StatusCode: statusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil)

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	a, err := New(
		WithHTTPClient(mockClient),
	)

	if err != nil {
		log.Fatal("Error creating agent")
	}

	return a
}

func TestNewAgent_ReturnsAgent(t *testing.T) {
	configResponse := func() (int, []byte) {
		cfg := struct {
			BaseURL       string   `json:"base_url"`
			EventsPath    string   `json:"events_path"`
			TargetRoutes  []string `json:"target"`
			SampledRoutes []string `json:"sampled"`
		}{
			BaseURL:       "https://dev-api.auditr.io/v1",
			EventsPath:    "/events",
			TargetRoutes:  []string{"POST /events", "PUT /events/:id"},
			SampledRoutes: []string{"GET /events", "GET /events/:id"},
		}
		responseBody, _ := json.Marshal(cfg)
		statusCode := 200

		return statusCode, responseBody
	}

	m := &mockTransport{
		fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
			m.MethodCalled("RoundTrip", req)

			var statusCode int
			var responseBody []byte
			switch req.URL.String() {
			case config.ConfigURL:
				statusCode, responseBody = configResponse()
			}

			r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

			return &http.Response{
				StatusCode: statusCode,
				Body:       r,
			}, nil
		},
	}

	m.
		On("RoundTrip", mock.AnythingOfType("*http.Request")).
		Return(mock.AnythingOfType("*http.Response"), nil)

	mockClient := func(ctx context.Context) *http.Client {
		return &http.Client{
			Transport: m,
		}
	}

	a, err := New(
		WithHTTPClient(mockClient),
	)
	assert.NoError(t, err)
	assert.NotNil(t, a)
}

// func TestWrapOld(t *testing.T) {
// 	postEvent := fmt.Sprintf(`{
// 		"resource": "/{proxy+}",
// 		  "path": "/hello/world",
// 		  "httpMethod": "POST",
// 		  "headers": {
// 			  "Accept": "*/*",
// 			  "Accept-Encoding": "gzip, deflate",
// 			  "cache-control": "no-cache",
// 			  "CloudFront-Forwarded-Proto": "https",
// 			  "CloudFront-Is-Desktop-Viewer": "true",
// 			  "CloudFront-Is-Mobile-Viewer": "false",
// 			  "CloudFront-Is-SmartTV-Viewer": "false",
// 			  "CloudFront-Is-Tablet-Viewer": "false",
// 			  "CloudFront-Viewer-Country": "US",
// 			  "Content-Type": "application/json",
// 			  "headerName": "headerValue",
// 			  "Host": "gy415nuibc.execute-api.us-east-1.amazonaws.com",
// 			  "Postman-Token": "9f583ef0-ed83-4a38-aef3-eb9ce3f7a57f",
// 			  "User-Agent": "PostmanRuntime/2.4.5",
// 			  "Via": "1.1 d98420743a69852491bbdea73f7680bd.cloudfront.net (CloudFront)",
// 			  "X-Amz-Cf-Id": "pn-PWIJc6thYnZm5P0NMgOUglL1DYtl0gdeJky8tqsg8iS_sgsKD1A==",
// 			  "X-Forwarded-For": "54.240.196.186, 54.182.214.83",
// 			  "X-Forwarded-Port": "443",
// 			  "X-Forwarded-Proto": "https"
// 		},
// 		"multiValueHeaders": {
// 			"Accept": ["*/*"],
// 			"Accept-Encoding": ["gzip, deflate"],
// 			"cache-control": ["no-cache"],
// 			"CloudFront-Forwarded-Proto": ["https"],
// 			"CloudFront-Is-Desktop-Viewer": ["true"],
// 			"CloudFront-Is-Mobile-Viewer": ["false"],
// 			"CloudFront-Is-SmartTV-Viewer": ["false"],
// 			"CloudFront-Is-Tablet-Viewer": ["false"],
// 			"CloudFront-Viewer-Country": ["US"],
// 			"Content-Type": ["application/json"],
// 			"headerName": ["headerValue"],
// 			"Host": ["gy415nuibc.execute-api.us-east-1.amazonaws.com"],
// 			"Postman-Token": ["9f583ef0-ed83-4a38-aef3-eb9ce3f7a57f"],
// 			"User-Agent": ["PostmanRuntime/2.4.5"],
// 			"Via": ["1.1 d98420743a69852491bbdea73f7680bd.cloudfront.net (CloudFront)"],
// 			"X-Amz-Cf-Id": ["pn-PWIJc6thYnZm5P0NMgOUglL1DYtl0gdeJky8tqsg8iS_sgsKD1A=="],
// 			"X-Forwarded-For": ["54.240.196.186, 54.182.214.83"],
// 			"X-Forwarded-Port": ["443"],
// 			"X-Forwarded-Proto": ["https"]
// 		},
// 		"queryStringParameters": {
// 			"name": "me"
// 		},
// 		"multiValueQueryStringParameters": {
// 			"name": ["me"]
// 		},
// 		"pathParameters": {
// 			"proxy": "hello/world"
// 		},
// 		"stageVariables": {
// 			"stageVariableName": "stageVariableValue"
// 		},
// 		"requestContext": {
// 			"accountId": "12345678912",
// 			"resourceId": "roq9wj",
// 			"stage": "testStage",
// 			"domainName": "gy415nuibc.execute-api.us-east-2.amazonaws.com",
// 			"domainPrefix": "y0ne18dixk",
// 			"requestId": "deef4878-7910-11e6-8f14-25afc3e9ae33",
// 			"protocol": "HTTP/1.1",
// 			"identity": {
// 				"cognitoIdentityPoolId": "theCognitoIdentityPoolId",
// 				"accountId": "theAccountId",
// 				"cognitoIdentityId": "theCognitoIdentityId",
// 				"caller": "theCaller",
// 				"apiKey": "theApiKey",
// 				"apiKeyId": "theApiKeyId",
// 				"accessKey": "ANEXAMPLEOFACCESSKEY",
// 				"sourceIp": "192.168.196.186",
// 				"cognitoAuthenticationType": "theCognitoAuthenticationType",
// 				"cognitoAuthenticationProvider": "theCognitoAuthenticationProvider",
// 				"userArn": "theUserArn",
// 				"userAgent": "PostmanRuntime/2.4.5",
// 				"user": "theUser"
// 			},
// 			"authorizer": {
// 				"principalId": "admin",
// 				"clientId": 1,
// 				"clientName": "Exata"
// 			},
// 			"resourcePath": "/{proxy+}",
// 			"httpMethod": "POST",
// 			"requestTime": "15/May/2020:06:01:09 +0000",
// 			"requestTimeEpoch": %d,
// 			"apiId": "gy415nuibc"
// 		},
// 		"body": "{\r\n\t\"a\": 1\r\n}"
// 	}`, time.Now().Unix())

// 	getEvent := fmt.Sprintf(`{
// 		"resource": "/events/{id}",
// 		"path": "/events/%[1]s",
// 		"httpMethod": "GET",
// 		"headers": {
// 			"Accept": "*/*",
// 			"Accept-Encoding": "gzip, deflate, br",
// 			"CloudFront-Forwarded-Proto": "https",
// 			"CloudFront-Is-Desktop-Viewer": "true",
// 			"CloudFront-Is-Mobile-Viewer": "false",
// 			"CloudFront-Is-SmartTV-Viewer": "false",
// 			"CloudFront-Is-Tablet-Viewer": "false",
// 			"CloudFront-Viewer-Country": "US",
// 			"Host": "dlu6td3jn1.execute-api.us-west-2.amazonaws.com",
// 			"Postman-Token": "a29e645b-cbc6-44cf-b41c-e0d5454fb864",
// 			"User-Agent": "PostmanRuntime/7.26.8",
// 			"Via": "1.1 d3e84a8f73f8d6438930c5b709821f40.cloudfront.net (CloudFront)",
// 			"X-Amz-Cf-Id": "EZf3zFDGkyMm63OoKczbdyFbxQIQFBDOp4yPMlA2oAiWY_vShXCbHg==",
// 			"X-Amzn-Trace-Id": "Root=1-5fb08245-3fa9fcea50ea73a54edf4194",
// 			"X-Forwarded-For": "192.168.196.186",
// 			"X-Forwarded-Port": "443",
// 			"X-Forwarded-Proto": "https"
// 		},
// 		"queryStringParameters": null,
// 		"pathParameters": {
// 			"id": "%[1]s"
// 		},
// 		"stageVariables": null,
// 		"requestContext": {
// 			"accountId": "000000000000",
// 			"resourceId": "ezww6a",
// 			"stage": "dev",
// 			"requestId": "1c2e6d39-7da4-476a-9e89-4ad51c950f55",
// 			"identity": {
// 				"cognitoIdentityPoolId": "",
// 				"accountId": "",
// 				"cognitoIdentityId": "",
// 				"caller": "",
// 				"apiKey": "",
// 				"sourceIp": "192.168.196.186",
// 				"cognitoAuthenticationType": "",
// 				"cognitoAuthenticationProvider": "",
// 				"userArn": "",
// 				"userAgent": "PostmanRuntime/7.26.8",
// 				"user": ""
// 			},
// 			"resourcePath": "/events/{id}",
// 			"authorizer": null,
// 			"httpMethod": "GET",
// 			"apiId": "dlu6td3jn1"
// 		},
// 		"body": ""
// 	}`, "1jnAFJlXHOm25Czq0CeZ8OrHA2n")

// 	resp := &Response{
// 		Hello: "homer",
// 	}

// 	for i, event := range []string{postEvent, getEvent} {
// 		e := events.APIGatewayProxyRequest{}
// 		json.Unmarshal([]byte(event), &e)

// 		t.Run(fmt.Sprintf("testCase %d", i), func(t *testing.T) {
// 			handler := func(ctx context.Context, request events.APIGatewayProxyRequest) (*Response, error) {
// 				return resp, nil
// 			}

// 			configResponse := func() (int, []byte) {
// 				cfg := struct {
// 					BaseURL       string   `json:"base_url"`
// 					EventsPath    string   `json:"events_path"`
// 					TargetRoutes  []string `json:"target"`
// 					SampledRoutes []string `json:"sampled"`
// 				}{
// 					BaseURL:       "https://dev-api.auditr.io/v1",
// 					EventsPath:    "/events",
// 					TargetRoutes:  []string{"POST /events", "PUT /events/:id"},
// 					SampledRoutes: []string{"GET /events", "GET /events/:id"},
// 				}
// 				responseBody, _ := json.Marshal(cfg)
// 				statusCode := 200

// 				return statusCode, responseBody
// 			}

// 			m := &mockTransport{
// 				fn: func(m *mockTransport, req *http.Request) (*http.Response, error) {
// 					m.MethodCalled("RoundTrip", req)

// 					var statusCode int
// 					var responseBody []byte
// 					switch req.URL.String() {
// 					case config.ConfigURL:
// 						statusCode, responseBody = configResponse()
// 					}

// 					r := ioutil.NopCloser(bytes.NewBuffer(responseBody))

// 					return &http.Response{
// 						StatusCode: statusCode,
// 						Body:       r,
// 					}, nil
// 				},
// 			}

// 			m.
// 				On("RoundTrip", mock.AnythingOfType("*http.Request")).
// 				Return(mock.AnythingOfType("*http.Response"), nil)

// 			mockClient := func(ctx context.Context) *http.Client {
// 				return &http.Client{
// 					Transport: m,
// 				}
// 			}

// 			a, err := New(
// 				WithHTTPClient(mockClient),
// 			)
// 			assert.NoError(t, err)
// 			lambdaHandler := a.WrapOld(handler)
// 			h := lambdaHandler.(func(context.Context, events.APIGatewayProxyRequest) (interface{}, error))
// 			f := lambdaFunction(h)

// 			// e, _ := json.Marshal(event)
// 			response, _ := f.invoke(context.TODO(), e)
// 			log.Println(response)
// 			// assert.Equal(t, resp.Hello, response)
// 		})
// 	}
// }

// func (handler lambdaFunction) invoke(ctx context.Context, request events.APIGatewayProxyRequest) ([]byte, error) {
// 	response, err := handler(ctx, request)
// 	if err != nil {
// 		return nil, err
// 	}

// 	responseBytes, err := json.Marshal(response)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return responseBytes, nil
// }
