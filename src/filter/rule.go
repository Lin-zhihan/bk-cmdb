/*
 * Tencent is pleased to support the open source community by making
 * 蓝鲸智云 - 配置平台 (BlueKing - Configuration System) available.
 * Copyright (C) 2017 THL A29 Limited,
 * a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on
 * an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the
 * specific language governing permissions and limitations under the License.
 * We undertake not to change the open source license (MIT license) applicable
 * to the current version of the project delivered to anyone in the future.
 */

package filter

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/criteria/enumor"
	"configcenter/src/common/util"

	"go.mongodb.org/mongo-driver/bson"
)

// RuleFactory defines an expression's basic rule, which is used to filter the resources.
type RuleFactory interface {
	// WithType get a rule's type
	WithType() RuleType
	// Validate this rule is valid or not
	Validate(opt *ExprOption) error
	// RuleFields get this rule's fields
	RuleFields() []string
	// ToMgo convert this rule to a mongo condition
	ToMgo(opt ...*RuleOption) (map[string]interface{}, error)
}

// RuleType is the expression rule's rule type.
type RuleType string

const (
	// UnknownType means it's an unknown type.
	UnknownType RuleType = "Unknown"
	// AtomType means it's an AtomRule
	AtomType RuleType = "AtomRule"
	// CombinedType means it's a CombinedRule
	CombinedType RuleType = "CombinedRule"
)

// RuleOption defines the options of a rule.
type RuleOption struct {
	// Parent field name, used when filtering object/array elements
	Parent string
	// ParentType parent type, used when filtering object/array elements
	ParentType enumor.ColumnType
}

var _ RuleFactory = new(AtomRule)

// AtomRule is the basic query rule.
type AtomRule struct {
	Field    string      `json:"field" bson:"field"`
	Operator OpFactory   `json:"operator" bson:"operator"`
	Value    interface{} `json:"value" bson:"value"`
}

// WithType return the atom rule's type.
func (ar *AtomRule) WithType() RuleType {
	return AtomType
}

// Validate this atom rule is valid or not
// Note: opt can be nil, check it before using it.
func (ar *AtomRule) Validate(opt *ExprOption) error {
	if len(ar.Field) == 0 {
		return errors.New("field is empty")
	}

	// validate operator
	if err := ar.Operator.Validate(); err != nil {
		return err
	}

	if ar.Value == nil {
		return errors.New("rule value can not be nil")
	}

	if opt != nil && len(opt.RuleFields) > 0 {
		// TODO confirm how to deal with object and array
		typ, exist := opt.RuleFields[ar.Field]
		if !exist {
			return fmt.Errorf("rule field: %s is not exist in the expr option", ar.Field)
		}

		if err := validateFieldValue(ar.Value, typ); err != nil {
			return fmt.Errorf("invalid %s's value, %v", ar.Field, err)
		}
	}

	// validate the operator's value
	if err := ar.Operator.Operator().ValidateValue(ar.Value, opt); err != nil {
		return fmt.Errorf("%s validate failed, %v", ar.Field, err)
	}

	return nil
}

func validateFieldValue(v interface{}, typ enumor.ColumnType) error {
	switch reflect.TypeOf(v).Kind() {
	case reflect.Array, reflect.Slice:
		return validateSliceElements(v, typ)
	default:
	}

	switch typ {
	case enumor.String:
		if reflect.ValueOf(v).Type().Kind() != reflect.String {
			return errors.New("value should be a string")
		}

	case enumor.Numeric:
		if !util.IsNumeric(v) {
			return errors.New("value should be a numeric")
		}

	case enumor.Boolean:
		if reflect.ValueOf(v).Type().Kind() != reflect.Bool {
			return errors.New("value should be a boolean")
		}

	case enumor.Time:
		if err := util.ValidateDatetimeType(v); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported value type format: %s", typ)
	}

	return nil
}

func validateSliceElements(v interface{}, typ enumor.ColumnType) error {
	value := reflect.ValueOf(v)
	length := value.Len()
	if length == 0 {
		return nil
	}

	// validate each slice's element data type
	for i := 0; i < length; i++ {
		if err := validateFieldValue(value.Index(i).Interface(), typ); err != nil {
			return err
		}
	}

	return nil
}

// RuleFields get atom rule's field
func (ar *AtomRule) RuleFields() []string {
	switch ar.Operator {
	// TODO confirm how to deal with these
	case OpFactory(FilterObject), OpFactory(FilterArray):
		// filter object and array operator's fields are its sub-rule fields with its prefix.
		subRule, ok := ar.Value.(RuleFactory)
		if !ok {
			blog.Errorf("%s operator's value(%+v) is not a rule type", ar.Operator, ar.Value)
			return []string{ar.Field}
		}

		subFields := subRule.RuleFields()

		fields := make([]string, len(subFields))
		for idx, field := range subFields {
			fields[idx] = ar.Field + "." + field
		}

		return fields
	}
	return []string{ar.Field}
}

// ToMgo convert this atom rule to a mongo query condition.
func (ar *AtomRule) ToMgo(opts ...*RuleOption) (map[string]interface{}, error) {
	if len(opts) > 0 && opts[0] != nil {
		opt := opts[0]
		if len(opt.Parent) == 0 {
			return nil, errors.New("parent is empty")
		}

		switch opt.ParentType {
		case enumor.Object:
			// add object parent field as prefix to generate object filter rules
			return ar.Operator.Operator().ToMgo(opt.Parent+"."+ar.Field, ar.Value)
		case enumor.Array:
			switch ar.Field {
			case FilterArrayElement:
				// filter array element, matches if any of the elements matches the filter
				return ar.Operator.Operator().ToMgo(opt.Parent, ar.Value)
			default:
				// filter specific element of array by index specified in field
				index, err := strconv.Atoi(ar.Field)
				if err != nil {
					return nil, fmt.Errorf("parse filter array index %s failed, err: %v", ar.Field, err)
				}

				if index <= 0 {
					return nil, fmt.Errorf("filter array index %d is invalid", index)
				}

				return ar.Operator.Operator().ToMgo(opt.Parent+"."+ar.Field, ar.Value)
			}
		default:
			return nil, fmt.Errorf("parent type %s is invalid", opt.ParentType)
		}
	}

	return ar.Operator.Operator().ToMgo(ar.Field, ar.Value)
}

type jsonAtomRuleBroker struct {
	Field    string          `json:"field"`
	Operator OpFactory       `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

// UnmarshalJSON unmarshal the json raw message to AtomRule
func (ar *AtomRule) UnmarshalJSON(raw []byte) error {
	br := new(jsonAtomRuleBroker)
	err := json.Unmarshal(raw, br)
	if err != nil {
		return err
	}

	ar.Field = br.Field
	ar.Operator = br.Operator
	switch br.Operator {
	case OpFactory(In), OpFactory(NotIn):
		// in and nin operator's value should be an array.
		array := make([]interface{}, 0)
		if err := json.Unmarshal(br.Value, &array); err != nil {
			return err
		}

		ar.Value = array
		return nil
	case OpFactory(FilterObject), OpFactory(FilterArray):
		// filter object and array operator's value should be a rule.
		subRule, err := parseJsonRule(br.Value)
		if err != nil {
			return err
		}
		ar.Value = subRule
		return nil
	}

	to := new(interface{})
	if err := json.Unmarshal(br.Value, to); err != nil {
		return err
	}
	ar.Value = *to

	return nil
}

type bsonAtomRuleBroker struct {
	Field    string        `bson:"field"`
	Operator OpFactory     `bson:"operator"`
	Value    bson.RawValue `bson:"value"`
}

type bsonAtomRuleCopier struct {
	Field    string      `bson:"field"`
	Operator OpFactory   `bson:"operator"`
	Value    interface{} `bson:"value"`
}

// MarshalBSON marshal the AtomRule to bson raw message
func (ar *AtomRule) MarshalBSON() ([]byte, error) {
	// right now bson will panic if MarshalBSON is defined using a value receiver and called by a nil pointer
	// TODO this is compatible for nil pointer, but struct marshalling is not supported, find a way to support both
	if ar == nil {
		return bson.Marshal(map[string]interface{}(nil))
	}

	b := bsonAtomRuleCopier{
		Field:    ar.Field,
		Operator: ar.Operator,
		Value:    ar.Value,
	}
	return bson.Marshal(b)
}

// UnmarshalBSON unmarshal the bson raw message to AtomRule
func (ar *AtomRule) UnmarshalBSON(raw []byte) error {
	br := new(bsonAtomRuleBroker)
	err := bson.Unmarshal(raw, br)
	if err != nil {
		return err
	}

	ar.Field = br.Field
	ar.Operator = br.Operator
	switch br.Operator {
	case OpFactory(In), OpFactory(NotIn):
		// in and nin operator's value should be an array.
		array := make([]interface{}, 0)
		if err := br.Value.Unmarshal(&array); err != nil {
			return err
		}

		ar.Value = array
		return nil
	case OpFactory(FilterObject), OpFactory(FilterArray):
		// filter object and array operator's value should be a rule.
		subRule, err := parseBsonRule(br.Value.Document())
		if err != nil {
			return err
		}
		ar.Value = subRule
		return nil
	}

	to := new(interface{})
	if err := br.Value.Unmarshal(to); err != nil {
		return err
	}
	ar.Value = *to

	return nil
}

var _ RuleFactory = new(CombinedRule)

// CombinedRule is the compound query rule combined by many rules.
type CombinedRule struct {
	Condition LogicOperator `json:"condition" bson:"condition"`
	Rules     []RuleFactory `json:"rules" bson:"rules"`
}

// WithType return the combined rule's tye.
func (cr *CombinedRule) WithType() RuleType {
	return CombinedType
}

// Validate the combined rule
// Note: opt can be nil, check it before using it.
func (cr *CombinedRule) Validate(opt *ExprOption) error {
	if err := cr.Condition.Validate(); err != nil {
		return err
	}

	if len(cr.Rules) == 0 {
		return errors.New("combined rules shouldn't be empty")
	}

	maxRules := DefaultMaxRuleLimit
	if opt != nil && opt.MaxRulesLimit > 0 {
		maxRules = opt.MaxRulesLimit
	}

	if len(cr.Rules) > int(maxRules) {
		return fmt.Errorf("rules elements number is overhead, it at most have %d rules", maxRules)
	}

	fieldsReminder := make(map[string]bool)
	for _, field := range cr.RuleFields() {
		fieldsReminder[field] = true
	}

	if len(fieldsReminder) == 0 {
		return errors.New("invalid expression, no field is found to query")
	}

	if opt != nil && len(opt.RuleFields) > 0 {
		reminder := make(map[string]bool)
		for col := range opt.RuleFields {
			reminder[col] = true
		}

		// all the rule's field should exist in the reminder.
		for one := range fieldsReminder {
			if exist := reminder[one]; !exist {
				return fmt.Errorf("expression rules field(%s) should not exist(not supported)", one)
			}
		}
	}

	// validate combined rule depth, then continues to validate children rule depth
	var childOpt *ExprOption
	if opt != nil && opt.MaxRulesDepth > 0 {
		if opt.MaxRulesDepth == 1 {
			return fmt.Errorf("expression rules depth exceeds maximum")
		}

		childOpt = &ExprOption{
			RuleFields:    opt.RuleFields,
			MaxInLimit:    opt.MaxInLimit,
			MaxNotInLimit: opt.MaxNotInLimit,
			MaxRulesLimit: opt.MaxRulesLimit,
			MaxRulesDepth: opt.MaxRulesDepth - 1,
		}
	}

	for _, one := range cr.Rules {
		if err := one.Validate(childOpt); err != nil {
			return err
		}
	}

	return nil
}

// RuleFields get combined rule's fields
func (cr *CombinedRule) RuleFields() []string {
	fields := make([]string, 0)
	for _, rule := range cr.Rules {
		fields = append(fields, rule.RuleFields()...)
	}
	return fields
}

// ToMgo convert the combined rule to a mongo query condition.
func (cr *CombinedRule) ToMgo(opt ...*RuleOption) (map[string]interface{}, error) {
	if err := cr.Condition.Validate(); err != nil {
		return nil, err
	}

	if len(cr.Rules) == 0 {
		return nil, errors.New("combined rules shouldn't be empty")
	}

	filters := make([]map[string]interface{}, 0)
	for idx, rule := range cr.Rules {
		filter, err := rule.ToMgo(opt...)
		if err != nil {
			return nil, fmt.Errorf("rules[%d] is invalid, err: %v", idx, err)
		}
		filters = append(filters, filter)
	}

	switch cr.Condition {
	case Or:
		return map[string]interface{}{common.BKDBOR: filters}, nil
	case And:
		return map[string]interface{}{common.BKDBAND: filters}, nil
	default:
		return nil, fmt.Errorf("unexpected operator %s", cr.Condition)
	}
}

type jsonCombinedRuleBroker struct {
	Condition LogicOperator     `json:"condition"`
	Rules     []json.RawMessage `json:"rules"`
}

// UnmarshalJSON unmarshal the json raw message to AtomRule
func (cr *CombinedRule) UnmarshalJSON(raw []byte) error {
	broker := new(jsonCombinedRuleBroker)

	err := json.Unmarshal(raw, broker)
	if err != nil {
		return fmt.Errorf("unmarshal into combined rule failed, err: %v", err)
	}

	cr.Condition = broker.Condition
	cr.Rules = make([]RuleFactory, len(broker.Rules))

	for idx, rawRule := range broker.Rules {
		rule, err := parseJsonRule(rawRule)
		if err != nil {
			return fmt.Errorf("parse rules[%d] %s failed, err: %v", idx, string(rawRule), err)
		}
		cr.Rules[idx] = rule
	}

	return nil
}

type bsonCombinedRuleBroker struct {
	Condition LogicOperator `bson:"condition"`
	Rules     []bson.Raw    `bson:"rules"`
}

// MarshalBSON marshal the bson raw message to CombinedRule
func (cr *CombinedRule) MarshalBSON() ([]byte, error) {
	// right now bson will panic if MarshalBSON is defined using a value receiver and called by a nil pointer
	// TODO this is compatible for nil pointer, but struct marshalling is not supported, find a way to support both
	if cr == nil {
		return bson.Marshal(map[string]interface{}(nil))
	}

	b := bsonCombinedRuleBroker{
		Condition: cr.Condition,
		Rules:     make([]bson.Raw, len(cr.Rules)),
	}

	for index, value := range cr.Rules {
		bsonVal, err := bson.Marshal(value)
		if err != nil {
			return nil, err
		}
		b.Rules[index] = bsonVal
	}

	return bson.Marshal(b)
}

// UnmarshalBSON unmarshal the bson raw message to CombinedRule
func (cr *CombinedRule) UnmarshalBSON(raw []byte) error {
	broker := new(bsonCombinedRuleBroker)

	err := bson.Unmarshal(raw, broker)
	if err != nil {
		return fmt.Errorf("unmarshal into combined rule failed, err: %v", err)
	}

	cr.Condition = broker.Condition
	cr.Rules = make([]RuleFactory, len(broker.Rules))

	for idx, rawRule := range broker.Rules {
		rule, err := parseBsonRule(rawRule)
		if err != nil {
			return fmt.Errorf("parse rules[%d] %s failed, err: %v", idx, string(rawRule), err)
		}
		cr.Rules[idx] = rule
	}

	return nil
}
