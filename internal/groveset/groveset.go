// Package groveset lists the columns a grove model-based UPDATE writes, with
// their values. A store uses it to build that SET by hand when one column has
// to take an expression, such as version = version + 1, which a model-based
// UPDATE cannot express and which explicit Set calls replace wholesale.
package groveset

import (
	"fmt"
	"reflect"
	"slices"
	"sync"

	"github.com/xraph/grove/schema"
)

// Column is one column of the SET and the value it is set to.
type Column struct {
	Name  string
	Value any
}

var tables sync.Map // reflect.Type -> *schema.Table

// Columns returns the columns a model-based UPDATE of model would set, in
// field order, leaving out any named in skip. It picks fields the way the
// grove drivers do: every mapped field except the primary key, scanonly and
// autoincrement fields. Values are passed as the fields hold them, which is
// also what the drivers bind for a field without nullzero.
func Columns(model any, skip ...string) ([]Column, error) {
	val := reflect.ValueOf(model)
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil, fmt.Errorf("groveset: nil %T", model)
		}
		val = val.Elem()
	}
	t, err := table(val.Type())
	if err != nil {
		return nil, err
	}
	cols := make([]Column, 0, len(t.Fields))
	for _, f := range t.Fields {
		o := f.Options
		if o.IsPK || o.ScanOnly || o.AutoIncrement || slices.Contains(skip, o.Column) {
			continue
		}
		cols = append(cols, Column{Name: o.Column, Value: val.FieldByIndex(f.GoIndex).Interface()})
	}
	return cols, nil
}

func table(typ reflect.Type) (*schema.Table, error) {
	if v, ok := tables.Load(typ); ok {
		if t, ok := v.(*schema.Table); ok {
			return t, nil
		}
	}
	t, err := schema.NewTable(reflect.New(typ).Interface())
	if err != nil {
		return nil, fmt.Errorf("groveset: %w", err)
	}
	tables.Store(typ, t)
	return t, nil
}
