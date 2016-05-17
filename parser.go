// Copyright 2016 Steven Oud. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be found
// in the LICENSE file.

package mathcat

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parser holds the lexed tokens, token position and declared variables. By
// default, variables always contains the constants defined below. These can
// however be overwritten.
type Parser struct {
	tokens    []*Token
	pos       int
	Variables map[string]float64
	tok       *Token
}

var (
	errDivionByZero         = errors.New("Divison by zero")
	errUnmatchedParentheses = errors.New("Unmatched parentheses")
	errInvalidSyntax        = errors.New("Invalid syntax")
)

// Some useful predefined variables that can be used in expressions. These
// can be overwritten.
var constants = map[string]float64{
	"pi":  math.Pi,
	"tau": math.Pi / 2,
	"phi": math.Phi,
	"e":   math.E,
}

// New initializes a new Parser instance, useful when you want to run multiple
// expression and/or use variables.
func New() *Parser {
	return &Parser{
		pos:       0,
		Variables: constants,
	}
}

// Eval evaluates an expression and returns its result and any errors found.
//
// Example:
//     res, err := mathcat.Eval("2 * 2 * 2") // 8
func Eval(expr string) (float64, error) {
	tokens, err := Lex(expr)

	// If a lexer error occured don't parse
	if err != nil {
		return -1, err
	}

	p := &Parser{
		tokens:    tokens,
		pos:       0,
		Variables: constants,
	}

	return p.parse()
}

// Run executes an expression on an existing parser instance. Useful for
// variable assignment.
//
// Example:
//     p.Run("a = 555")
//     p.Run("a += 45")
//     res, err := p.Run("a + a") // 1200
func (p *Parser) Run(expr string) (float64, error) {
	tokens, err := Lex(expr)

	if err != nil {
		return -1, err
	}

	p.reset()
	p.tokens = tokens

	return p.parse()
}

// Exec executes an expression with a given map of variables.
//
// Example:
//     res, err := mathcat.Exec("a + b * b", map[string]float64{
//         "a": 1,
//         "b": 3,
//     }) // 10
func Exec(expr string, vars map[string]float64) (float64, error) {
	tokens, err := Lex(expr)

	if err != nil {
		return -1, err
	}

	p := &Parser{
		tokens:    tokens,
		pos:       0,
		Variables: constants,
	}

	isValidIdent := func(c rune) bool { return isIdent(c) || isNumber(c) }

	for k, v := range vars {
		if !isIdent(rune(k[0])) || strings.IndexFunc(k, isValidIdent) == -1 {
			return -1, fmt.Errorf("Invalid variable name: '%s'")
		}
		p.Variables[k] = v
	}

	p.tokens = tokens

	return p.parse()
}

// GetVar gets an existing variable.
func (p *Parser) GetVar(index string) (float64, error) {
	if val, ok := p.Variables[index]; ok {
		return val, nil
	}

	return -1, fmt.Errorf("Undefined variable '%s'", index)
}

func (p *Parser) parse() (float64, error) {
	var (
		operands, operators stack
		o1, o2              operator
	)

	p.tok = p.tokens[0]

	for p.eat().Type != EOL {
		switch {
		case p.tok.IsLiteral():
			if p.peek().Type == LPAREN {
				// It's a function call, push to operators stack instead
				operators.Push(p.tok)
				break
			}
			operands.Push(p.tok)
		case p.tok.Type == LPAREN:
			operators.Push(p.tok)
		case p.tok.IsOperator():
			o1 = ops[p.tok.Type]

			if !operators.Empty() {
				var ok bool
				if o2, ok = ops[operators.Top().(*Token).Type]; !ok {
					operators.Push(p.tok)
					break
				}

				if o2.hasHigherPrecThan(o1) {
					operator := operators.Pop().(*Token)
					val, err := p.evaluateOp(operator, &operands)
					if err != nil {
						return -1, err
					}
					operands.Push(val)
				}
			}
			operators.Push(p.tok)
		case p.tok.Type == RPAREN:
			for {
				if operators.Empty() {
					return -1, errUnmatchedParentheses
				}

				operator := operators.Pop().(*Token)
				if operator.Type == LPAREN {
					break
				}

				val, err := p.evaluateOp(operator, &operands)
				if err != nil {
					return -1, err
				}
				operands.Push(val)
			}
		}
	}

	// Evaluate remaing operators
	for !operators.Empty() {
		operator := operators.Pop().(*Token)

		if operator.Type == LPAREN {
			return -1, errUnmatchedParentheses
		}

		if operands.Empty() {
			return -1, errInvalidSyntax
		}

		val, err := p.evaluateOp(operator, &operands)
		if err != nil {
			return -1, err
		}
		operands.Push(val)
	}

	// If there are no operands, the expression is useless and doesn't do
	// anything, for example `()` or an empty string
	if operands.Empty() {
		return 0, nil
	}

	// Single operand left means the expression was evaluated successful
	if len(operands) == 1 {
		return p.lookup(operands[0])
	}

	// Leftover token on operand stack indicates invalid syntax
	return -1, errInvalidSyntax
}

func (p *Parser) evaluateOp(operator *Token, operands *stack) (float64, error) {
	var (
		result      float64
		left, right float64
		err         error
		lhsToken    interface{}
	)

	if operands.Empty() {
		return -1, fmt.Errorf("Unexpected '%s'", operator.Type)
	}

	if right, err = p.lookup(operands.Pop()); err != nil {
		return -1, err
	}

	// Unary operators have no left hand side
	if op := ops[operator.Type]; !op.unary {
		if operands.Empty() {
			return -1, errInvalidSyntax
		}
		// Save the token in case of a assignment variable is used and we need to
		// save the result in a variable
		lhsToken = operands.Pop()

		// Don't lookup the left hand side if = is used so we can do initial
		// assignment
		if operator.Type != EQ {
			left, err = p.lookup(lhsToken)
			if err != nil {
				return -1, err
			}
		}
	}

	result, err = execute(operator, left, right)
	if err != nil {
		return -1, err
	}

	switch operator.Type {
	case EQ, ADD_EQ, SUB_EQ, DIV_EQ, MUL_EQ, POW_EQ, REM_EQ, AND_EQ, OR_EQ, XOR_EQ, LSH_EQ, RSH_EQ:
		// Save result in variable
		if lhsToken.(*Token).Type != IDENT {
			return -1, errors.New("Can't assign to literal")
		}
		p.Variables[lhsToken.(*Token).Value] = result
	}

	return result, nil
}

func execute(operator *Token, lhs, rhs float64) (float64, error) {
	var result float64

	// Both lhs and rhs have to be whole numbers for bitwise operations
	if operator.IsBitwise() && (!IsWholeNumber(lhs) || !IsWholeNumber(rhs)) {
		return -1, fmt.Errorf("Unsupported type (float) for '%s'", operator.Type)
	}

	switch operator.Type {
	case ADD, ADD_EQ:
		result = lhs + rhs
	case SUB, SUB_EQ:
		result = lhs - rhs
	case UNARY_MIN:
		result = -rhs
	case DIV, DIV_EQ:
		if rhs == 0 {
			return -1, errDivionByZero
		}
		result = lhs / rhs
	case MUL, MUL_EQ:
		result = lhs * rhs
	case POW, POW_EQ:
		result = math.Pow(lhs, rhs)
	case REM, REM_EQ:
		if rhs == 0 {
			return -1, errDivionByZero
		}
		result = math.Mod(lhs, rhs)
	case AND, AND_EQ:
		result = float64(int64(lhs) & int64(rhs))
	case OR, OR_EQ:
		result = float64(int64(lhs) | int64(rhs))
	case XOR, XOR_EQ:
		result = float64(int64(lhs) ^ int64(rhs))
	case LSH, LSH_EQ:
		result = float64(uint64(lhs) << uint64(rhs))
	case RSH, RSH_EQ:
		result = float64(uint64(lhs) >> uint64(rhs))
	case NOT:
		result = float64(^int64(rhs))
	case EQ:
		result = rhs
	case EQ_EQ:
		result = bool2float(lhs == rhs)
	case GT:
		result = bool2float(lhs > rhs)
	case GT_EQ:
		result = bool2float(lhs >= rhs)
	case LT:
		result = bool2float(lhs < rhs)
	case LT_EQ:
		result = bool2float(lhs <= rhs)
	default:
		return -1, fmt.Errorf("Invalid operator '%s'", operator.Type)
	}

	return result, nil
}

// Look up a literal. If it's an identifier, check the parser's variables map,
// otherwise convert the tokenized string to a float64.
func (p *Parser) lookup(val interface{}) (float64, error) {
	// val can be a token or a float64, if it's a float64 it has been already
	// evaluated and we don't need to do anything
	if v, ok := val.(float64); ok {
		return v, nil
	}

	tok := val.(*Token)
	switch tok.Type {
	case NUMBER:
		res, err := strconv.ParseFloat(tok.Value, 64)
		if err != nil {
			return -1, fmt.Errorf("Error parsing '%s': invalid syntax", tok.Value)
		}

		return res, nil
	case HEX:
		// Remove 0x part of hex literal and convert to uint first
		res, err := strconv.ParseUint(tok.Value[2:], 16, 64)
		if err != nil {
			return -1, fmt.Errorf("Error parsing '%s': invalid syntax", tok.Value)
		}

		// Then convert to float
		return float64(res), nil
	case BINARY:
		// Remove 0b part of binary literal and convert to uint first
		res, err := strconv.ParseUint(tok.Value[2:], 2, 64)
		if err != nil {
			return -1, fmt.Errorf("Error parsing '%s': invalid syntax", tok.Value)
		}

		// Then convert to float
		return float64(res), nil
	case IDENT:
		res, err := p.GetVar(tok.Value)
		if err != nil {
			return -1, err
		}

		return res, nil
	}

	return -1, fmt.Errorf("Invalid lookup type: %s", tok.Type)
}

func (p *Parser) reset() {
	p.tokens = nil
	p.pos = 0
}

func (p *Parser) peek() *Token {
	return p.tokens[p.pos]
}

func (p *Parser) eat() *Token {
	p.tok = p.peek()
	p.pos++
	return p.tok
}

// IsWholeNumber checks if a float is a whole number
func IsWholeNumber(n float64) bool {
	epsilon := 1e-9
	_, frac := math.Modf(math.Abs(n))

	return frac < epsilon || frac > 1.0-epsilon
}

func bool2float(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
