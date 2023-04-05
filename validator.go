package validator

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	tagName = "validate"

	lenTag = "len"
	inTag  = "in"
	minTag = "min"
	maxTag = "max"
)

var ErrNotStruct = errors.New("wrong argument given, should be a struct")
var ErrInvalidValidatorSyntax = errors.New("invalid validator syntax")
var ErrValidateForUnexportedFields = errors.New("validation for unexported field is not allowed")

type ValidationError struct {
	Err error
}

type ValidationErrors []ValidationError

func (v ValidationErrors) Error() string {
	var sb strings.Builder

	for _, verr := range v {
		sb.WriteString(fmt.Sprintf("%s: ", verr.Err.Error()))
	}

	return strings.TrimSuffix(sb.String(), ": ")
}

func (v ValidationErrors) Is(target error) bool {
	for _, verr := range v {
		if errors.Is(verr.Err, target) {
			return true
		}
	}
	return false
}

type validationFunc func(value reflect.Value) error

func getLenValidationFunc(length int) validationFunc {
	return func(value reflect.Value) error {
		switch value.Kind() {
		case reflect.String:
			if utf8.RuneCountInString(value.String()) != length {
				return errors.New("invalid length")
			}
			return nil
		case reflect.Slice, reflect.Array:
			if value.Len() != length {
				return errors.New("invalid length")
			}
			return nil
		}
		return errors.New("invalid type of field for tag len")
	}
}

func getInValidationFunc(strs []string) validationFunc {
	return func(value reflect.Value) error {
		switch value.Kind() {
		case reflect.String:
			for _, str := range strs {
				if value.String() == strings.TrimSpace(str) {
					return nil
				}
			}
			return errors.New("field value is not in array from tag")
		case reflect.Int:
			in := make([]int64, 0, len(strs))
			for _, str := range strs {
				value, err := strconv.ParseInt(strings.TrimSpace(str), 0, 64)
				if err != nil {
					return ErrInvalidValidatorSyntax
				}
				in = append(in, value)
			}

			for _, v := range in {
				if v == value.Int() {
					return nil
				}
			}
			return errors.New("field value is not in array from tag")
		}
		return errors.New("invalid type of field for tag in")
	}
}

func getMinValidationFunc(min int64) validationFunc {
	return func(value reflect.Value) error {
		switch value.Kind() {
		case reflect.String:
			if int64(utf8.RuneCountInString(value.String())) < min {
				return errors.New("string length less than min")
			}
			return nil
		case reflect.Slice, reflect.Array:
			if int64(value.Len()) < min {
				return errors.New("slice length less than min")
			}
			return nil
		case reflect.Int:
			if value.Int() < min {
				return errors.New("int value less than min")
			}
			return nil
		}
		return errors.New("invalid type of field for tag min")
	}
}

func getMaxValidationFunc(max int64) validationFunc {
	return func(value reflect.Value) error {
		switch value.Kind() {
		case reflect.String:
			if int64(utf8.RuneCountInString(value.String())) > max {
				return errors.New("string length greater than max")
			}
			return nil
		case reflect.Slice, reflect.Array:
			if int64(value.Len()) > max {
				return errors.New("slice length greater than max")
			}
			return nil
		case reflect.Int:
			if value.Int() > max {
				return errors.New("int value greater than max")
			}
			return nil
		}
		return errors.New("invalid type of field for tag max")
	}
}

func getValidationFunc(funcName string, param string) (validationFunc, error) {
	funcName = strings.TrimSpace(funcName)
	param = strings.TrimSpace(param)
	if param == "" {
		return nil, ErrInvalidValidatorSyntax
	}

	switch funcName {
	case lenTag:
		length, err := strconv.Atoi(param)
		if err != nil {
			return nil, ErrInvalidValidatorSyntax
		}
		return getLenValidationFunc(length), nil
	case inTag:
		strs := strings.Split(param, ",")

		return getInValidationFunc(strs), nil
	case minTag:
		min, err := strconv.ParseInt(param, 0, 64)
		if err != nil {
			return nil, ErrInvalidValidatorSyntax
		}
		return getMinValidationFunc(min), nil
	case maxTag:
		max, err := strconv.ParseInt(param, 0, 64)
		if err != nil {
			return nil, ErrInvalidValidatorSyntax
		}
		return getMaxValidationFunc(max), nil
	}
	return nil, ErrInvalidValidatorSyntax
}

func Validate(v any) error {
	if errs := deepValidate(reflect.ValueOf(v), nil); len(errs) > 0 {
		return errs
	}

	return nil
}

func deepValidate(value reflect.Value, errs ValidationErrors) ValidationErrors {
	switch value.Kind() {
	case reflect.Ptr:
		if value.IsNil() {
			return errs
		}

		return deepValidate(value.Elem(), errs)
	case reflect.Struct:
		return validateStruct(value, errs)
	case reflect.Array, reflect.Slice:
		switch value.Type().Elem().Kind() {
		case reflect.Struct, reflect.Ptr, reflect.Array, reflect.Slice:
			for i := 0; i < value.Len(); i++ {
				errs = deepValidate(value.Index(i), errs)
			}
			return errs
		}
	}

	return append(errs, ValidationError{Err: ErrNotStruct})
}

func validateStruct(value reflect.Value, errs ValidationErrors) ValidationErrors {
	valueKind := value.Kind()
	if valueKind == reflect.Ptr && !value.IsNil() {
		return validateStruct(value.Elem(), errs)
	}
	if valueKind != reflect.Struct {
		return append(errs, ValidationError{Err: ErrNotStruct})
	}

	valueType := value.Type()
	for i := 0; i < valueType.NumField(); i++ {
		errs = validateField(valueType.Field(i), value.Field(i), errs)
	}

	return errs
}

func validateField(fieldDefinition reflect.StructField, fieldValue reflect.Value, errs ValidationErrors) ValidationErrors {
	tag := fieldDefinition.Tag.Get(tagName)
	if tag == "" || tag == "-" {
		return errs
	}

	if !fieldDefinition.Anonymous && fieldDefinition.PkgPath != "" {
		return append(errs, ValidationError{Err: ErrValidateForUnexportedFields})
	}

	vfunc, err := parseValidateTag(tag)
	if err != nil {
		return append(errs, ValidationError{Err: err})
	}

	for fieldValue.Kind() == reflect.Ptr && !fieldValue.IsNil() {
		fieldValue = fieldValue.Elem()
	}

	if err := vfunc(fieldValue); err != nil {
		errs = append(errs, ValidationError{Err: err})
	}

	return errs
}

func parseValidateTag(validateTag string) (validationFunc, error) {
	s := strings.SplitN(validateTag, ":", 2)

	return getValidationFunc(s[0], s[1])
}
