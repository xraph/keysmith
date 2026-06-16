package postgres

type notFoundError struct{ entity string }

func (e *notFoundError) Error() string { return e.entity + " not found" }

func errNotFound(entity string) error { return &notFoundError{entity: entity} }

// jsonbObject guarantees a non-nil map so an empty jsonb object ('{}') is
// stored instead of SQL NULL. The keysmith_* tables declare jsonb columns as
// NOT NULL DEFAULT '{}', but the DEFAULT never kicks in: grove always lists the
// column in the INSERT, so a nil map reaches pgx as an explicit NULL and trips
// the not-null constraint.
func jsonbObject(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// jsonbArray guarantees a non-nil slice so an empty jsonb array ('[]') is
// stored instead of SQL NULL, for the same reason as jsonbObject.
func jsonbArray(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
