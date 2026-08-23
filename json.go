package sql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

type Json[T any] struct {
	value T
}

func NewJson[T any](value T) *Json[T] {
	return &Json[T]{value}
}

func (f *Json[T]) Value() (driver.Value, error) {
	if f == nil {
		return nil, nil
	}

	switch any(f.value).(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		return driver.DefaultParameterConverter.ConvertValue(f.value)
	}

	return json.Marshal(f.value)
}

func (f *Json[T]) Scan(value any) error {
	var v []byte
	switch vv := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
		v = []byte(fmt.Sprintf("%v", value))
	case string:
		v = []byte(vv)
	default:
		v = value.([]byte)
	}
	return json.Unmarshal(v, &f.value)
}

func (f *Json[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.value)
}

func (f *Json[T]) UnmarshalJSON(value []byte) error {
	return json.Unmarshal(value, &f.value)
}

func (f *Json[T]) Unwrap() T {
	if f == nil {
		return *new(T)
	}
	return f.value
}

func (j *Json[T]) ScanText(v pgtype.Text) error {
	if !v.Valid {
		j.value = *new(T)
		return nil
	}
	return json.Unmarshal([]byte(v.String), &j.value)
}

func (j Json[T]) TextValue() (pgtype.Text, error) {
	data, err := json.Marshal(j.value)
	if err != nil {
		return pgtype.Text{}, err
	}
	return pgtype.Text{String: string(data), Valid: true}, nil
}
