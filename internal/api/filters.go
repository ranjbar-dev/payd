package api

import (
	"errors"
	"net/url"
	"strconv"
)

func unixRange(values url.Values, required bool) (*int64, *int64, error) {
	parse := func(name string) (*int64, error) {
		value := values.Get(name)
		if value == "" {
			if required {
				return nil, errors.New("from and to are required Unix timestamps")
			}
			return nil, nil
		}
		stamp, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, errors.New(name + " must be a Unix timestamp")
		}
		return &stamp, nil
	}
	from, err := parse("from")
	if err != nil {
		return nil, nil, err
	}
	to, err := parse("to")
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && *from > *to {
		return nil, nil, errors.New("from must not exceed to")
	}
	return from, to, nil
}
