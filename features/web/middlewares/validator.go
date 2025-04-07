package middlewares

import (
	"errors"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
)

// Error variables for validator
var (
	ErrValidationFailed = errors.New("validation failed")
)

type Validator struct {
	validator *validator.Validate
}

func (v *Validator) Validate(i interface{}) error {
	err := v.validator.Struct(i)
	if err == nil {
		return nil
	}

	errs := err.(validator.ValidationErrors)
	if len(errs) == 0 {
		return nil
	}

	errorMsgs := make([]string, 0, len(errs))
	for _, v := range errs {
		errorMsgs = append(errorMsgs, v.Error())
	}

	msg := strings.Join(errorMsgs, ", ")
	log.Error().Str("validation_errors", msg).Msg("Request validation failed")
	return ErrValidationFailed
}

func ConfigureValidator(e *echo.Echo) {
	e.Validator = &Validator{validator: validator.New()}
}
