package auditr

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/assert"
)

func TestAudit(t *testing.T) {
	id := "xyz"
	req := events.APIGatewayProxyRequest{
		PathParameters: map[string]string{"id": id},
	}

	body := fmt.Sprintf(`{"id": %s}`, id)
	expected := events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       body,
	}

	handler := func(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		r, _ := json.Marshal(request)
		fmt.Println("request: ", string(r))

		return expected, nil
	}

	h := Audit(handler).(func(context.Context, events.APIGatewayProxyRequest) (interface{}, error))
	resp, _ := h(context.Background(), req)
	assert.Equal(t, expected, resp, "responses mismatch")

}
