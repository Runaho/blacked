package middlewares

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
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
	msg := ""
	for _, v := range errs {
		if msg != "" {
			msg += ", "
		}
		msg += v.Error()
	}
	return errors.New(msg)
}

func ConfigureValidator(e *echo.Echo) {
	e.Validator = &Validator{validator: validator.New()}
}
