package validate_test

import (
	"testing"

	"github.com/gopherex/xconf/pkg/validate"
)

func TestNumeric(t *testing.T) {
	if err := validate.Range(1, 10)(5); err != nil {
		t.Errorf("Range pass: %v", err)
	}
	if err := validate.Range(1, 10)(11); err == nil {
		t.Errorf("Range fail expected")
	}
	if err := validate.Min(3)(2); err == nil {
		t.Errorf("Min fail expected")
	}
	if err := validate.Max(3)(4); err == nil {
		t.Errorf("Max fail expected")
	}
	if err := validate.Positive[int]()(0); err == nil {
		t.Errorf("Positive(0) should fail")
	}
	if err := validate.NonNegative[int]()(-1); err == nil {
		t.Errorf("NonNegative(-1) should fail")
	}
	if err := validate.NonZero[int]()(0); err == nil {
		t.Errorf("NonZero(0) should fail")
	}
}

func TestStrings(t *testing.T) {
	if err := validate.NonEmpty()(""); err == nil {
		t.Errorf("NonEmpty empty should fail")
	}
	if err := validate.MinLen(3)("ab"); err == nil {
		t.Errorf("MinLen fail expected")
	}
	if err := validate.MaxLen(3)("abcd"); err == nil {
		t.Errorf("MaxLen fail expected")
	}
	if err := validate.Regex(`^\d+$`)("abc"); err == nil {
		t.Errorf("Regex fail expected")
	}
	if err := validate.HasPrefix("foo")("bar"); err == nil {
		t.Errorf("HasPrefix fail expected")
	}
	if err := validate.URL()("not a url"); err == nil {
		t.Errorf("URL fail expected")
	}
	if err := validate.URL()("https://example.com"); err != nil {
		t.Errorf("URL pass: %v", err)
	}
	if err := validate.Email()("foo@bar.com"); err != nil {
		t.Errorf("Email pass: %v", err)
	}
	if err := validate.Email()("nope"); err == nil {
		t.Errorf("Email fail expected")
	}
}

func TestCollection(t *testing.T) {
	if err := validate.MinItems[int](2)([]int{1}); err == nil {
		t.Errorf("MinItems fail expected")
	}
	if err := validate.Unique[int]()([]int{1, 1}); err == nil {
		t.Errorf("Unique fail expected")
	}
	if err := validate.Each(validate.Positive[int]())([]int{1, -1}); err == nil {
		t.Errorf("Each should propagate inner failure")
	}
}

func TestCombinators(t *testing.T) {
	v := validate.All(validate.Min(0), validate.Max(10))
	if err := v(5); err != nil {
		t.Errorf("All pass: %v", err)
	}
	if err := v(11); err == nil {
		t.Errorf("All fail expected")
	}
	any := validate.Any(validate.Equal(1), validate.Equal(2))
	if err := any(3); err == nil {
		t.Errorf("Any fail expected")
	}
	if err := any(2); err != nil {
		t.Errorf("Any pass: %v", err)
	}
	not := validate.Not(validate.Equal(0), "must not be zero")
	if err := not(0); err == nil {
		t.Errorf("Not fail expected")
	}
}
