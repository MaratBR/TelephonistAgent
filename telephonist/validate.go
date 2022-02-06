package telephonist

import (
	"github.com/go-playground/validator/v10"
)

var (
	Validator = validator.New()
)

type ValidateSelf interface {
	Validate() error
}

func initValidator() {
	Validator.RegisterValidation("validate_self", func(v validator.FieldLevel) bool {
		if validate, ok := v.Field().Interface().(ValidateSelf); ok {
			return validate.Validate() == nil
		}

		return true
	})
}
