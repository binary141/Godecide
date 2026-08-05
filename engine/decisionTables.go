package engine

// Single hit policies

const HitPolicyUnique = "UNIQUE"
const HitPolicyFirst = "FIRST"
const HitPolicyAny = "ANY"
const HitPolicyPriority = "PRIORITY"

// Multiple hit policies

const HitPolicyOutputOrder = "OUTPUT ORDER"
const HitPolicyRuleOrder = "RULE ORDER"
const HitPolicyCollect = "COLLECT"

// COLLECT aggregation functions

const AggregationSum = "SUM"
const AggregationMin = "MIN"
const AggregationMax = "MAX"
const AggregationCount = "COUNT"

func IsValidHitPolicy(hitPolicy string) bool {
	switch hitPolicy {
	case HitPolicyUnique, HitPolicyFirst, HitPolicyAny, HitPolicyPriority:
		return true
	case HitPolicyOutputOrder, HitPolicyRuleOrder, HitPolicyCollect:
		return true
	default:
		return false
	}
}
