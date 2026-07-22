package apiv1

import "github.com/gin-gonic/gin"

// errorBody is the JSON envelope every failing API response uses. A stable,
// machine-readable `code` lets the mobile clients switch on the failure without
// parsing the human-facing `message`.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeError aborts the request with the JSON error envelope.
func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}
