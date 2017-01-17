// Do not edit. Bootstrap copy of /home/yanni/biscuit/golang1.7/src/math/big/ftoa.go

//line /home/yanni/biscuit/golang1.7/src/math/big/ftoa.go:1
// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file implements Float-to-string conversion functions.
// It is closely following the corresponding implementation
// in strconv/ftoa.go, but modified and simplified for Float.

package big

import (
	"bytes"
	"fmt"
	"strconv"
)

// Text converts the floating-point number x to a string according
// to the given format and precision prec. The format is one of:
//
//	'e'	-d.dddde±dd, decimal exponent, at least two (possibly 0) exponent digits
//	'E'	-d.ddddE±dd, decimal exponent, at least two (possibly 0) exponent digits
//	'f'	-ddddd.dddd, no exponent
//	'g'	like 'e' for large exponents, like 'f' otherwise
//	'G'	like 'E' for large exponents, like 'f' otherwise
//	'b'	-ddddddp±dd, binary exponent
//	'p'	-0x.dddp±dd, binary exponent, hexadecimal mantissa
//
// For the binary exponent formats, the mantissa is printed in normalized form:
//
//	'b'	decimal integer mantissa using x.Prec() bits, or -0
//	'p'	hexadecimal fraction with 0.5 <= 0.mantissa < 1.0, or -0
//
// If format is a different character, Text returns a "%" followed by the
// unrecognized format character.
//
// The precision prec controls the number of digits (excluding the exponent)
// printed by the 'e', 'E', 'f', 'g', and 'G' formats. For 'e', 'E', and 'f'
// it is the number of digits after the decimal point. For 'g' and 'G' it is
// the total number of digits. A negative precision selects the smallest
// number of decimal digits necessary to identify the value x uniquely using
// x.Prec() mantissa bits.
// The prec value is ignored for the 'b' or 'p' format.
func (x *Float) Text(format byte, prec int) string {
	cap := 10 // TODO(gri) determine a good/better value here
	if prec > 0 {
		cap += prec
	}
	return string(x.Append(make([]byte, 0, cap), format, prec))
}

// String formats x like x.Text('g', 10).
// (String must be called explicitly, Float.Format does not support %s verb.)
func (x *Float) String() string {
	return x.Text('g', 10)
}

// Append appends to buf the string form of the floating-point number x,
// as generated by x.Text, and returns the extended buffer.
func (x *Float) Append(buf []byte, fmt byte, prec int) []byte {
	// sign
	if x.neg {
		buf = append(buf, '-')
	}

	// Inf
	if x.form == inf {
		if !x.neg {
			buf = append(buf, '+')
		}
		return append(buf, "Inf"...)
	}

	// pick off easy formats
	switch fmt {
	case 'b':
		return x.fmtB(buf)
	case 'p':
		return x.fmtP(buf)
	}

	// Algorithm:
	//   1) convert Float to multiprecision decimal
	//   2) round to desired precision
	//   3) read digits out and format

	// 1) convert Float to multiprecision decimal
	var d decimal // == 0.0
	if x.form == finite {
		// x != 0
		d.init(x.mant, int(x.exp)-x.mant.bitLen())
	}

	// 2) round to desired precision
	shortest := false
	if prec < 0 {
		shortest = true
		roundShortest(&d, x)
		// Precision for shortest representation mode.
		switch fmt {
		case 'e', 'E':
			prec = len(d.mant) - 1
		case 'f':
			prec = max(len(d.mant)-d.exp, 0)
		case 'g', 'G':
			prec = len(d.mant)
		}
	} else {
		// round appropriately
		switch fmt {
		case 'e', 'E':
			// one digit before and number of digits after decimal point
			d.round(1 + prec)
		case 'f':
			// number of digits before and after decimal point
			d.round(d.exp + prec)
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			d.round(prec)
		}
	}

	// 3) read digits out and format
	switch fmt {
	case 'e', 'E':
		return fmtE(buf, fmt, prec, d)
	case 'f':
		return fmtF(buf, prec, d)
	case 'g', 'G':
		// trim trailing fractional zeros in %e format
		eprec := prec
		if eprec > len(d.mant) && len(d.mant) >= d.exp {
			eprec = len(d.mant)
		}
		// %e is used if the exponent from the conversion
		// is less than -4 or greater than or equal to the precision.
		// If precision was the shortest possible, use eprec = 6 for
		// this decision.
		if shortest {
			eprec = 6
		}
		exp := d.exp - 1
		if exp < -4 || exp >= eprec {
			if prec > len(d.mant) {
				prec = len(d.mant)
			}
			return fmtE(buf, fmt+'e'-'g', prec-1, d)
		}
		if prec > d.exp {
			prec = len(d.mant)
		}
		return fmtF(buf, max(prec-d.exp, 0), d)
	}

	// unknown format
	if x.neg {
		buf = buf[:len(buf)-1] // sign was added prematurely - remove it again
	}
	return append(buf, '%', fmt)
}

func roundShortest(d *decimal, x *Float) {
	// if the mantissa is zero, the number is zero - stop now
	if len(d.mant) == 0 {
		return
	}

	// Approach: All numbers in the interval [x - 1/2ulp, x + 1/2ulp]
	// (possibly exclusive) round to x for the given precision of x.
	// Compute the lower and upper bound in decimal form and find the
	// shortest decimal number d such that lower <= d <= upper.

	// TODO(gri) strconv/ftoa.do describes a shortcut in some cases.
	// See if we can use it (in adjusted form) here as well.

	// 1) Compute normalized mantissa mant and exponent exp for x such
	// that the lsb of mant corresponds to 1/2 ulp for the precision of
	// x (i.e., for mant we want x.prec + 1 bits).
	mant := nat(nil).set(x.mant)
	exp := int(x.exp) - mant.bitLen()
	s := mant.bitLen() - int(x.prec+1)
	switch {
	case s < 0:
		mant = mant.shl(mant, uint(-s))
	case s > 0:
		mant = mant.shr(mant, uint(+s))
	}
	exp += s
	// x = mant * 2**exp with lsb(mant) == 1/2 ulp of x.prec

	// 2) Compute lower bound by subtracting 1/2 ulp.
	var lower decimal
	var tmp nat
	lower.init(tmp.sub(mant, natOne), exp)

	// 3) Compute upper bound by adding 1/2 ulp.
	var upper decimal
	upper.init(tmp.add(mant, natOne), exp)

	// The upper and lower bounds are possible outputs only if
	// the original mantissa is even, so that ToNearestEven rounding
	// would round to the original mantissa and not the neighbors.
	inclusive := mant[0]&2 == 0 // test bit 1 since original mantissa was shifted by 1

	// Now we can figure out the minimum number of digits required.
	// Walk along until d has distinguished itself from upper and lower.
	for i, m := range d.mant {
		l := lower.at(i)
		u := upper.at(i)

		// Okay to round down (truncate) if lower has a different digit
		// or if lower is inclusive and is exactly the result of rounding
		// down (i.e., and we have reached the final digit of lower).
		okdown := l != m || inclusive && i+1 == len(lower.mant)

		// Okay to round up if upper has a different digit and either upper
		// is inclusive or upper is bigger than the result of rounding up.
		okup := m != u && (inclusive || m+1 < u || i+1 < len(upper.mant))

		// If it's okay to do either, then round to the nearest one.
		// If it's okay to do only one, do it.
		switch {
		case okdown && okup:
			d.round(i + 1)
			return
		case okdown:
			d.roundDown(i + 1)
			return
		case okup:
			d.roundUp(i + 1)
			return
		}
	}
}

// %e: d.ddddde±dd
func fmtE(buf []byte, fmt byte, prec int, d decimal) []byte {
	// first digit
	ch := byte('0')
	if len(d.mant) > 0 {
		ch = d.mant[0]
	}
	buf = append(buf, ch)

	// .moredigits
	if prec > 0 {
		buf = append(buf, '.')
		i := 1
		m := min(len(d.mant), prec+1)
		if i < m {
			buf = append(buf, d.mant[i:m]...)
			i = m
		}
		for ; i <= prec; i++ {
			buf = append(buf, '0')
		}
	}

	// e±
	buf = append(buf, fmt)
	var exp int64
	if len(d.mant) > 0 {
		exp = int64(d.exp) - 1 // -1 because first digit was printed before '.'
	}
	if exp < 0 {
		ch = '-'
		exp = -exp
	} else {
		ch = '+'
	}
	buf = append(buf, ch)

	// dd...d
	if exp < 10 {
		buf = append(buf, '0') // at least 2 exponent digits
	}
	return strconv.AppendInt(buf, exp, 10)
}

// %f: ddddddd.ddddd
func fmtF(buf []byte, prec int, d decimal) []byte {
	// integer, padded with zeros as needed
	if d.exp > 0 {
		m := min(len(d.mant), d.exp)
		buf = append(buf, d.mant[:m]...)
		for ; m < d.exp; m++ {
			buf = append(buf, '0')
		}
	} else {
		buf = append(buf, '0')
	}

	// fraction
	if prec > 0 {
		buf = append(buf, '.')
		for i := 0; i < prec; i++ {
			buf = append(buf, d.at(d.exp+i))
		}
	}

	return buf
}

// fmtB appends the string of x in the format mantissa "p" exponent
// with a decimal mantissa and a binary exponent, or 0" if x is zero,
// and returns the extended buffer.
// The mantissa is normalized such that is uses x.Prec() bits in binary
// representation.
// The sign of x is ignored, and x must not be an Inf.
func (x *Float) fmtB(buf []byte) []byte {
	if x.form == zero {
		return append(buf, '0')
	}

	if debugFloat && x.form != finite {
		panic("non-finite float")
	}
	// x != 0

	// adjust mantissa to use exactly x.prec bits
	m := x.mant
	switch w := uint32(len(x.mant)) * _W; {
	case w < x.prec:
		m = nat(nil).shl(m, uint(x.prec-w))
	case w > x.prec:
		m = nat(nil).shr(m, uint(w-x.prec))
	}

	buf = append(buf, m.utoa(10)...)
	buf = append(buf, 'p')
	e := int64(x.exp) - int64(x.prec)
	if e >= 0 {
		buf = append(buf, '+')
	}
	return strconv.AppendInt(buf, e, 10)
}

// fmtP appends the string of x in the format "0x." mantissa "p" exponent
// with a hexadecimal mantissa and a binary exponent, or "0" if x is zero,
// and returns the extended buffer.
// The mantissa is normalized such that 0.5 <= 0.mantissa < 1.0.
// The sign of x is ignored, and x must not be an Inf.
func (x *Float) fmtP(buf []byte) []byte {
	if x.form == zero {
		return append(buf, '0')
	}

	if debugFloat && x.form != finite {
		panic("non-finite float")
	}
	// x != 0

	// remove trailing 0 words early
	// (no need to convert to hex 0's and trim later)
	m := x.mant
	i := 0
	for i < len(m) && m[i] == 0 {
		i++
	}
	m = m[i:]

	buf = append(buf, "0x."...)
	buf = append(buf, bytes.TrimRight(m.utoa(16), "0")...)
	buf = append(buf, 'p')
	if x.exp >= 0 {
		buf = append(buf, '+')
	}
	return strconv.AppendInt(buf, int64(x.exp), 10)
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

var _ fmt.Formatter = &floatZero // *Float must implement fmt.Formatter

// Format implements fmt.Formatter. It accepts all the regular
// formats for floating-point numbers ('b', 'e', 'E', 'f', 'F',
// 'g', 'G') as well as 'p' and 'v'. See (*Float).Text for the
// interpretation of 'p'. The 'v' format is handled like 'g'.
// Format also supports specification of the minimum precision
// in digits, the output field width, as well as the format flags
// '+' and ' ' for sign control, '0' for space or zero padding,
// and '-' for left or right justification. See the fmt package
// for details.
func (x *Float) Format(s fmt.State, format rune) {
	prec, hasPrec := s.Precision()
	if !hasPrec {
		prec = 6 // default precision for 'e', 'f'
	}

	switch format {
	case 'e', 'E', 'f', 'b', 'p':
		// nothing to do
	case 'F':
		// (*Float).Text doesn't support 'F'; handle like 'f'
		format = 'f'
	case 'v':
		// handle like 'g'
		format = 'g'
		fallthrough
	case 'g', 'G':
		if !hasPrec {
			prec = -1 // default precision for 'g', 'G'
		}
	default:
		fmt.Fprintf(s, "%%!%c(*big.Float=%s)", format, x.String())
		return
	}
	var buf []byte
	buf = x.Append(buf, byte(format), prec)
	if len(buf) == 0 {
		buf = []byte("?") // should never happen, but don't crash
	}
	// len(buf) > 0

	var sign string
	switch {
	case buf[0] == '-':
		sign = "-"
		buf = buf[1:]
	case buf[0] == '+':
		// +Inf
		sign = "+"
		if s.Flag(' ') {
			sign = " "
		}
		buf = buf[1:]
	case s.Flag('+'):
		sign = "+"
	case s.Flag(' '):
		sign = " "
	}

	var padding int
	if width, hasWidth := s.Width(); hasWidth && width > len(sign)+len(buf) {
		padding = width - len(sign) - len(buf)
	}

	switch {
	case s.Flag('0') && !x.IsInf():
		// 0-padding on left
		writeMultiple(s, sign, 1)
		writeMultiple(s, "0", padding)
		s.Write(buf)
	case s.Flag('-'):
		// padding on right
		writeMultiple(s, sign, 1)
		s.Write(buf)
		writeMultiple(s, " ", padding)
	default:
		// padding on left
		writeMultiple(s, " ", padding)
		writeMultiple(s, sign, 1)
		s.Write(buf)
	}
}
