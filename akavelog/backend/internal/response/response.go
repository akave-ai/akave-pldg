package response

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// APIResponse is the standard success response shape.
type APIResponse struct {
	Data    any    `json:"data"`
	Status  int    `json:"status"`
	Message string `json:"message,omitempty"`
	Path    string `json:"path"`
}

// APIError is the standard error response shape.
type APIError struct {
	Message string `json:"message"`
	Error   string `json:"error"`
	Path    string `json:"path"`
	Status  int    `json:"status"`
}

// pathFromContext returns the request path from Echo context.
func pathFromContext(c echo.Context) string {
	if c == nil || c.Request() == nil {
		return ""
	}
	return c.Request().URL.Path
}

// OK sends a 200 response with data.
func OK(c echo.Context, data any, message string) error {
	return c.JSON(http.StatusOK, APIResponse{
		Data:    data,
		Status:  http.StatusOK,
		Message: message,
		Path:    pathFromContext(c),
	})
}

// Created sends a 201 response with data.
func Created(c echo.Context, data any, message string) error {
	return c.JSON(http.StatusCreated, APIResponse{
		Data:    data,
		Status:  http.StatusCreated,
		Message: message,
		Path:    pathFromContext(c),
	})
}

// NoContent sends 204. For consistency you can use OK(c, nil, "Deleted") with 200 instead if you want a body.
func NoContent(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}

// Error sends a JSON error response using APIError.
func Error(c echo.Context, status int, message, errDetail string) error {
	return c.JSON(status, APIError{
		Message: message,
		Error:   errDetail,
		Path:    pathFromContext(c),
		Status:  status,
	})
}

// BadRequest sends 400 with message and error detail.
func BadRequest(c echo.Context, message, errDetail string) error {
	return Error(c, http.StatusBadRequest, message, errDetail)
}

// NotFound sends 404 with message and error detail.
func NotFound(c echo.Context, message, errDetail string) error {
	return Error(c, http.StatusNotFound, message, errDetail)
}

// InternalError sends 500 with message and error detail.
func InternalError(c echo.Context, message, errDetail string) error {
	return Error(c, http.StatusInternalServerError, message, errDetail)
}
