// Package condition is a test against a data map: a dot path and how its
// value must compare (equals, min, regex, typeOf, …), or a clock window
// (time, dayOfWeek, cron). Lists combine as when (all must pass) and or (one
// must pass).
//
//	when:
//	  - field: spec.environment
//	    equals: production
//	or:
//	  - field: spec.tier
//	    equals: gold
//	  - field: spec.replicas
//	    greaterThan: "3"
//
// A field or comparison value written as a template is resolved by the
// TemplateEvaluator the caller passes. The comparison table is exported so
// other declarations that compare a field the same way (a validation rule, a
// text assertion) share its names and operators.
package condition
