package condition

// UniquenessChecker reports whether no other object of the kind under test
// has the same value at a field. selfNamespace and selfName identify the
// object under test, so it is never a duplicate of itself.
type UniquenessChecker interface {
	IsUnique(field, value, selfNamespace, selfName string) (bool, error)
}

// UniquenessCheckerKey is the data key a caller puts its UniquenessChecker
// under for unique: true to use.
const UniquenessCheckerKey = "_uniquenessChecker"

// IsUnique evaluates unique: true for value at field with the checker in
// data, excluding the object by its metadata.namespace and metadata.name. It
// passes when data has no checker or the checker errors: uniqueness is
// enforced when it can be checked, not required.
func IsUnique(data map[string]interface{}, field, value string) bool {
	checker, ok := data[UniquenessCheckerKey].(UniquenessChecker)
	if !ok || checker == nil {
		return true
	}
	unique, err := checker.IsUnique(field, value,
		NavigateDotPath(data, "metadata.namespace"), NavigateDotPath(data, "metadata.name"))
	if err != nil {
		return true
	}
	return unique
}
