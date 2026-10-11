package api

import (
	"reflect"
	"sort"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestRolesFromClaims(t *testing.T) {
	c := jwt.MapClaims{
		"realm_access":    map[string]any{"roles": []any{"citric-admin", "offline"}},
		"resource_access": map[string]any{"citric": map[string]any{"roles": []any{"reviewer"}}, "other": map[string]any{"roles": []any{"nope"}}},
		"groups":          []any{"eng"},
		"app":             map[string]any{"roles": []any{"custom"}},
	}
	got := rolesFromClaims(c, "citric", "app.roles")
	sort.Strings(got)
	want := []string{"citric-admin", "custom", "eng", "offline", "reviewer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
