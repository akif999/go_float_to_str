// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Binary to decimal floating point conversion.
// Algorithm:
//   1) store mantissa in multiprecision decimal
//   2) shift decimal by exponent
//   3) read digits out & format

package main

import (
	"math"
	"math/bits"
)

type decimal struct {
	d     [800]byte // digits, big-endian representation
	nd    int       // number of digits used
	dp    int       // decimal point
	neg   bool      // negative flag
	trunc bool      // discarded nonzero digits beyond d[:nd]
}

// An extFloat represents an extended floating-point number, with more
// precision than a float64. It does not try to save bits: the
// number represented by the structure is mant*(2^exp), with a negative
// sign if neg is true.
type extFloat struct {
	mant uint64
	exp  int
	neg  bool
}

// TODO: move elsewhere?
type floatInfo struct {
	mantbits uint
	expbits  uint
	bias     int
}

const smallsString = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

const (
	lowerhex  = "0123456789abcdef"
	upperhex  = "0123456789ABCDEF"
	digits    = "0123456789abcdefghijklmnopqrstuvwxyz"
	host32bit = ^uint(0)>>32 == 0
)

const (
	firstPowerOfTen = -348
	stepPowerOfTen  = 8
)

var optimize = true // set to false to force slow-path conversions for testing

var float32info = floatInfo{23, 8, -127}
var float64info = floatInfo{52, 11, -1023}

var uint64pow10 = [...]uint64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

var powersOfTen = [...]extFloat{
	{0xfa8fd5a0081c0288, -1220, false}, // 10^-348
	{0xbaaee17fa23ebf76, -1193, false}, // 10^-340
	{0x8b16fb203055ac76, -1166, false}, // 10^-332
	{0xcf42894a5dce35ea, -1140, false}, // 10^-324
	{0x9a6bb0aa55653b2d, -1113, false}, // 10^-316
	{0xe61acf033d1a45df, -1087, false}, // 10^-308
	{0xab70fe17c79ac6ca, -1060, false}, // 10^-300
	{0xff77b1fcbebcdc4f, -1034, false}, // 10^-292
	{0xbe5691ef416bd60c, -1007, false}, // 10^-284
	{0x8dd01fad907ffc3c, -980, false},  // 10^-276
	{0xd3515c2831559a83, -954, false},  // 10^-268
	{0x9d71ac8fada6c9b5, -927, false},  // 10^-260
	{0xea9c227723ee8bcb, -901, false},  // 10^-252
	{0xaecc49914078536d, -874, false},  // 10^-244
	{0x823c12795db6ce57, -847, false},  // 10^-236
	{0xc21094364dfb5637, -821, false},  // 10^-228
	{0x9096ea6f3848984f, -794, false},  // 10^-220
	{0xd77485cb25823ac7, -768, false},  // 10^-212
	{0xa086cfcd97bf97f4, -741, false},  // 10^-204
	{0xef340a98172aace5, -715, false},  // 10^-196
	{0xb23867fb2a35b28e, -688, false},  // 10^-188
	{0x84c8d4dfd2c63f3b, -661, false},  // 10^-180
	{0xc5dd44271ad3cdba, -635, false},  // 10^-172
	{0x936b9fcebb25c996, -608, false},  // 10^-164
	{0xdbac6c247d62a584, -582, false},  // 10^-156
	{0xa3ab66580d5fdaf6, -555, false},  // 10^-148
	{0xf3e2f893dec3f126, -529, false},  // 10^-140
	{0xb5b5ada8aaff80b8, -502, false},  // 10^-132
	{0x87625f056c7c4a8b, -475, false},  // 10^-124
	{0xc9bcff6034c13053, -449, false},  // 10^-116
	{0x964e858c91ba2655, -422, false},  // 10^-108
	{0xdff9772470297ebd, -396, false},  // 10^-100
	{0xa6dfbd9fb8e5b88f, -369, false},  // 10^-92
	{0xf8a95fcf88747d94, -343, false},  // 10^-84
	{0xb94470938fa89bcf, -316, false},  // 10^-76
	{0x8a08f0f8bf0f156b, -289, false},  // 10^-68
	{0xcdb02555653131b6, -263, false},  // 10^-60
	{0x993fe2c6d07b7fac, -236, false},  // 10^-52
	{0xe45c10c42a2b3b06, -210, false},  // 10^-44
	{0xaa242499697392d3, -183, false},  // 10^-36
	{0xfd87b5f28300ca0e, -157, false},  // 10^-28
	{0xbce5086492111aeb, -130, false},  // 10^-20
	{0x8cbccc096f5088cc, -103, false},  // 10^-12
	{0xd1b71758e219652c, -77, false},   // 10^-4
	{0x9c40000000000000, -50, false},   // 10^4
	{0xe8d4a51000000000, -24, false},   // 10^12
	{0xad78ebc5ac620000, 3, false},     // 10^20
	{0x813f3978f8940984, 30, false},    // 10^28
	{0xc097ce7bc90715b3, 56, false},    // 10^36
	{0x8f7e32ce7bea5c70, 83, false},    // 10^44
	{0xd5d238a4abe98068, 109, false},   // 10^52
	{0x9f4f2726179a2245, 136, false},   // 10^60
	{0xed63a231d4c4fb27, 162, false},   // 10^68
	{0xb0de65388cc8ada8, 189, false},   // 10^76
	{0x83c7088e1aab65db, 216, false},   // 10^84
	{0xc45d1df942711d9a, 242, false},   // 10^92
	{0x924d692ca61be758, 269, false},   // 10^100
	{0xda01ee641a708dea, 295, false},   // 10^108
	{0xa26da3999aef774a, 322, false},   // 10^116
	{0xf209787bb47d6b85, 348, false},   // 10^124
	{0xb454e4a179dd1877, 375, false},   // 10^132
	{0x865b86925b9bc5c2, 402, false},   // 10^140
	{0xc83553c5c8965d3d, 428, false},   // 10^148
	{0x952ab45cfa97a0b3, 455, false},   // 10^156
	{0xde469fbd99a05fe3, 481, false},   // 10^164
	{0xa59bc234db398c25, 508, false},   // 10^172
	{0xf6c69a72a3989f5c, 534, false},   // 10^180
	{0xb7dcbf5354e9bece, 561, false},   // 10^188
	{0x88fcf317f22241e2, 588, false},   // 10^196
	{0xcc20ce9bd35c78a5, 614, false},   // 10^204
	{0x98165af37b2153df, 641, false},   // 10^212
	{0xe2a0b5dc971f303a, 667, false},   // 10^220
	{0xa8d9d1535ce3b396, 694, false},   // 10^228
	{0xfb9b7cd9a4a7443c, 720, false},   // 10^236
	{0xbb764c4ca7a44410, 747, false},   // 10^244
	{0x8bab8eefb6409c1a, 774, false},   // 10^252
	{0xd01fef10a657842c, 800, false},   // 10^260
	{0x9b10a4e5e9913129, 827, false},   // 10^268
	{0xe7109bfba19c0c9d, 853, false},   // 10^276
	{0xac2820d9623bf429, 880, false},   // 10^284
	{0x80444b5e7aa7cf85, 907, false},   // 10^292
	{0xbf21e44003acdd2d, 933, false},   // 10^300
	{0x8e679c2f5e44ff8f, 960, false},   // 10^308
	{0xd433179d9c8cb841, 986, false},   // 10^316
	{0x9e19db92b4e31ba9, 1013, false},  // 10^324
	{0xeb96bf6ebadf77d9, 1039, false},  // 10^332
	{0xaf87023b9bf0ee6b, 1066, false},  // 10^340
}

type leftCheat struct {
	delta  int    // number of new digits
	cutoff string // minus one digit if original < a.
}

var leftcheats = []leftCheat{
	// Leading digits of 1/2^i = 5^i.
	// 5^23 is not an exact 64-bit floating point number,
	// so have to use bc for the math.
	// Go up to 60 to be large enough for 32bit and 64bit platforms.
	/*
		seq 60 | sed 's/^/5^/' | bc |
		awk 'BEGIN{ print "\t{ 0, \"\" }," }
		{
			log2 = log(2)/log(10)
			printf("\t{ %d, \"%s\" },\t// * %d\n",
				int(log2*NR+1), $0, 2**NR)
		}'
	*/
	{0, ""},
	{1, "5"},                                           // * 2
	{1, "25"},                                          // * 4
	{1, "125"},                                         // * 8
	{2, "625"},                                         // * 16
	{2, "3125"},                                        // * 32
	{2, "15625"},                                       // * 64
	{3, "78125"},                                       // * 128
	{3, "390625"},                                      // * 256
	{3, "1953125"},                                     // * 512
	{4, "9765625"},                                     // * 1024
	{4, "48828125"},                                    // * 2048
	{4, "244140625"},                                   // * 4096
	{4, "1220703125"},                                  // * 8192
	{5, "6103515625"},                                  // * 16384
	{5, "30517578125"},                                 // * 32768
	{5, "152587890625"},                                // * 65536
	{6, "762939453125"},                                // * 131072
	{6, "3814697265625"},                               // * 262144
	{6, "19073486328125"},                              // * 524288
	{7, "95367431640625"},                              // * 1048576
	{7, "476837158203125"},                             // * 2097152
	{7, "2384185791015625"},                            // * 4194304
	{7, "11920928955078125"},                           // * 8388608
	{8, "59604644775390625"},                           // * 16777216
	{8, "298023223876953125"},                          // * 33554432
	{8, "1490116119384765625"},                         // * 67108864
	{9, "7450580596923828125"},                         // * 134217728
	{9, "37252902984619140625"},                        // * 268435456
	{9, "186264514923095703125"},                       // * 536870912
	{10, "931322574615478515625"},                      // * 1073741824
	{10, "4656612873077392578125"},                     // * 2147483648
	{10, "23283064365386962890625"},                    // * 4294967296
	{10, "116415321826934814453125"},                   // * 8589934592
	{11, "582076609134674072265625"},                   // * 17179869184
	{11, "2910383045673370361328125"},                  // * 34359738368
	{11, "14551915228366851806640625"},                 // * 68719476736
	{12, "72759576141834259033203125"},                 // * 137438953472
	{12, "363797880709171295166015625"},                // * 274877906944
	{12, "1818989403545856475830078125"},               // * 549755813888
	{13, "9094947017729282379150390625"},               // * 1099511627776
	{13, "45474735088646411895751953125"},              // * 2199023255552
	{13, "227373675443232059478759765625"},             // * 4398046511104
	{13, "1136868377216160297393798828125"},            // * 8796093022208
	{14, "5684341886080801486968994140625"},            // * 17592186044416
	{14, "28421709430404007434844970703125"},           // * 35184372088832
	{14, "142108547152020037174224853515625"},          // * 70368744177664
	{15, "710542735760100185871124267578125"},          // * 140737488355328
	{15, "3552713678800500929355621337890625"},         // * 281474976710656
	{15, "17763568394002504646778106689453125"},        // * 562949953421312
	{16, "88817841970012523233890533447265625"},        // * 1125899906842624
	{16, "444089209850062616169452667236328125"},       // * 2251799813685248
	{16, "2220446049250313080847263336181640625"},      // * 4503599627370496
	{16, "11102230246251565404236316680908203125"},     // * 9007199254740992
	{17, "55511151231257827021181583404541015625"},     // * 18014398509481984
	{17, "277555756156289135105907917022705078125"},    // * 36028797018963968
	{17, "1387778780781445675529539585113525390625"},   // * 72057594037927936
	{18, "6938893903907228377647697925567626953125"},   // * 144115188075855872
	{18, "34694469519536141888238489627838134765625"},  // * 288230376151711744
	{18, "173472347597680709441192448139190673828125"}, // * 576460752303423488
	{19, "867361737988403547205962240695953369140625"}, // * 1152921504606846976
}

// Is the leading prefix of b lexicographically less than s?
func prefixIsLessThan(b []byte, s string) bool {
	for i := 0; i < len(s); i++ {
		if i >= len(b) {
			return true
		}
		if b[i] != s[i] {
			return b[i] < s[i]
		}
	}
	return false
}

// FormatFloat converts the floating-point number f to a string,
// according to the format fmt and precision prec. It rounds the
// result assuming that the original was obtained from a floating-point
// value of bitSize bits (32 for float32, 64 for float64).
//
// The format fmt is one of
// 'b' (-ddddp±ddd, a binary exponent),
// 'e' (-d.dddde±dd, a decimal exponent),
// 'E' (-d.ddddE±dd, a decimal exponent),
// 'f' (-ddd.dddd, no exponent),
// 'g' ('e' for large exponents, 'f' otherwise),
// 'G' ('E' for large exponents, 'f' otherwise),
// 'x' (-0xd.ddddp±ddd, a hexadecimal fraction and binary exponent), or
// 'X' (-0Xd.ddddP±ddd, a hexadecimal fraction and binary exponent).
//
// The precision prec controls the number of digits (excluding the exponent)
// printed by the 'e', 'E', 'f', 'g', 'G', 'x', and 'X' formats.
// For 'e', 'E', 'f', 'x', and 'X', it is the number of digits after the decimal point.
// For 'g' and 'G' it is the maximum number of significant digits (trailing
// zeros are removed).
// The special precision -1 uses the smallest number of digits
// necessary such that ParseFloat will return f exactly.
func FormatFloat(f float64, fmt byte, prec, bitSize int) string {
	return string(genericFtoa(make([]byte, 0, max(prec+4, 24)), f, fmt, prec, bitSize))
}

// AppendFloat appends the string form of the floating-point number f,
// as generated by FormatFloat, to dst and returns the extended buffer.
func AppendFloat(dst []byte, f float64, fmt byte, prec, bitSize int) []byte {
	return genericFtoa(dst, f, fmt, prec, bitSize)
}

func genericFtoa(dst []byte, val float64, fmt byte, prec, bitSize int) []byte {
	var bits uint64
	var flt *floatInfo
	switch bitSize {
	case 32:
		bits = uint64(math.Float32bits(float32(val)))
		flt = &float32info
	case 64:
		bits = math.Float64bits(val)
		flt = &float64info
	default:
		panic("strconv: illegal AppendFloat/FormatFloat bitSize")
	}

	neg := bits>>(flt.expbits+flt.mantbits) != 0
	exp := int(bits>>flt.mantbits) & (1<<flt.expbits - 1)
	mant := bits & (uint64(1)<<flt.mantbits - 1)

	switch exp {
	case 1<<flt.expbits - 1:
		// Inf, NaN
		var s string
		switch {
		case mant != 0:
			s = "NaN"
		case neg:
			s = "-Inf"
		default:
			s = "+Inf"
		}
		return append(dst, s...)

	case 0:
		// denormalized
		exp++

	default:
		// add implicit top bit
		mant |= uint64(1) << flt.mantbits
	}
	exp += flt.bias

	// Pick off easy binary, hex formats.
	if fmt == 'b' {
		return fmtB(dst, neg, mant, exp, flt)
	}
	if fmt == 'x' || fmt == 'X' {
		return fmtX(dst, prec, fmt, neg, mant, exp, flt)
	}

	if !optimize {
		return bigFtoa(dst, prec, fmt, neg, mant, exp, flt)
	}

	var digs decimalSlice
	ok := false
	// Negative precision means "only as much as needed to be exact."
	shortest := prec < 0
	if shortest {
		// Try Grisu3 algorithm.
		f := new(extFloat)
		lower, upper := f.AssignComputeBounds(mant, exp, neg, flt)
		var buf [32]byte
		digs.d = buf[:]
		ok = f.ShortestDecimal(&digs, &lower, &upper)
		if !ok {
			return bigFtoa(dst, prec, fmt, neg, mant, exp, flt)
		}
		// Precision for shortest representation mode.
		switch fmt {
		case 'e', 'E':
			prec = max(digs.nd-1, 0)
		case 'f':
			prec = max(digs.nd-digs.dp, 0)
		case 'g', 'G':
			prec = digs.nd
		}
	} else if fmt != 'f' {
		// Fixed number of digits.
		digits := prec
		switch fmt {
		case 'e', 'E':
			digits++
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			digits = prec
		}
		if digits <= 15 {
			// try fast algorithm when the number of digits is reasonable.
			var buf [24]byte
			digs.d = buf[:]
			f := extFloat{mant, exp - int(flt.mantbits), neg}
			ok = f.FixedDecimal(&digs, digits)
		}
	}
	if !ok {
		return bigFtoa(dst, prec, fmt, neg, mant, exp, flt)
	}
	return formatDigits(dst, shortest, neg, digs, prec, fmt)
}

// bigFtoa uses multiprecision computations to format a float.
func bigFtoa(dst []byte, prec int, fmt byte, neg bool, mant uint64, exp int, flt *floatInfo) []byte {
	d := new(decimal)
	d.Assign(mant)
	d.Shift(exp - int(flt.mantbits))
	var digs decimalSlice
	shortest := prec < 0
	if shortest {
		roundShortest(d, mant, exp, flt)
		digs = decimalSlice{d: d.d[:], nd: d.nd, dp: d.dp}
		// Precision for shortest representation mode.
		switch fmt {
		case 'e', 'E':
			prec = digs.nd - 1
		case 'f':
			prec = max(digs.nd-digs.dp, 0)
		case 'g', 'G':
			prec = digs.nd
		}
	} else {
		// Round appropriately.
		switch fmt {
		case 'e', 'E':
			d.Round(prec + 1)
		case 'f':
			d.Round(d.dp + prec)
		case 'g', 'G':
			if prec == 0 {
				prec = 1
			}
			d.Round(prec)
		}
		digs = decimalSlice{d: d.d[:], nd: d.nd, dp: d.dp}
	}
	return formatDigits(dst, shortest, neg, digs, prec, fmt)
}

func formatDigits(dst []byte, shortest bool, neg bool, digs decimalSlice, prec int, fmt byte) []byte {
	switch fmt {
	case 'e', 'E':
		return fmtE(dst, neg, digs, prec, fmt)
	case 'f':
		return fmtF(dst, neg, digs, prec)
	case 'g', 'G':
		// trailing fractional zeros in 'e' form will be trimmed.
		eprec := prec
		if eprec > digs.nd && digs.nd >= digs.dp {
			eprec = digs.nd
		}
		// %e is used if the exponent from the conversion
		// is less than -4 or greater than or equal to the precision.
		// if precision was the shortest possible, use precision 6 for this decision.
		if shortest {
			eprec = 6
		}
		exp := digs.dp - 1
		if exp < -4 || exp >= eprec {
			if prec > digs.nd {
				prec = digs.nd
			}
			return fmtE(dst, neg, digs, prec-1, fmt+'e'-'g')
		}
		if prec > digs.dp {
			prec = digs.nd
		}
		return fmtF(dst, neg, digs, max(prec-digs.dp, 0))
	}

	// unknown format
	return append(dst, '%', fmt)
}

// roundShortest rounds d (= mant * 2^exp) to the shortest number of digits
// that will let the original floating point value be precisely reconstructed.
func roundShortest(d *decimal, mant uint64, exp int, flt *floatInfo) {
	// If mantissa is zero, the number is zero; stop now.
	if mant == 0 {
		d.nd = 0
		return
	}

	// Compute upper and lower such that any decimal number
	// between upper and lower (possibly inclusive)
	// will round to the original floating point number.

	// We may see at once that the number is already shortest.
	//
	// Suppose d is not denormal, so that 2^exp <= d < 10^dp.
	// The closest shorter number is at least 10^(dp-nd) away.
	// The lower/upper bounds computed below are at distance
	// at most 2^(exp-mantbits).
	//
	// So the number is already shortest if 10^(dp-nd) > 2^(exp-mantbits),
	// or equivalently log2(10)*(dp-nd) > exp-mantbits.
	// It is true if 332/100*(dp-nd) >= exp-mantbits (log2(10) > 3.32).
	minexp := flt.bias + 1 // minimum possible exponent
	if exp > minexp && 332*(d.dp-d.nd) >= 100*(exp-int(flt.mantbits)) {
		// The number is already shortest.
		return
	}

	// d = mant << (exp - mantbits)
	// Next highest floating point number is mant+1 << exp-mantbits.
	// Our upper bound is halfway between, mant*2+1 << exp-mantbits-1.
	upper := new(decimal)
	upper.Assign(mant*2 + 1)
	upper.Shift(exp - int(flt.mantbits) - 1)

	// d = mant << (exp - mantbits)
	// Next lowest floating point number is mant-1 << exp-mantbits,
	// unless mant-1 drops the significant bit and exp is not the minimum exp,
	// in which case the next lowest is mant*2-1 << exp-mantbits-1.
	// Either way, call it mantlo << explo-mantbits.
	// Our lower bound is halfway between, mantlo*2+1 << explo-mantbits-1.
	var mantlo uint64
	var explo int
	if mant > 1<<flt.mantbits || exp == minexp {
		mantlo = mant - 1
		explo = exp
	} else {
		mantlo = mant*2 - 1
		explo = exp - 1
	}
	lower := new(decimal)
	lower.Assign(mantlo*2 + 1)
	lower.Shift(explo - int(flt.mantbits) - 1)

	// The upper and lower bounds are possible outputs only if
	// the original mantissa is even, so that IEEE round-to-even
	// would round to the original mantissa and not the neighbors.
	inclusive := mant%2 == 0

	// As we walk the digits we want to know whether rounding up would fall
	// within the upper bound. This is tracked by upperdelta:
	//
	// If upperdelta == 0, the digits of d and upper are the same so far.
	//
	// If upperdelta == 1, we saw a difference of 1 between d and upper on a
	// previous digit and subsequently only 9s for d and 0s for upper.
	// (Thus rounding up may fall outside the bound, if it is exclusive.)
	//
	// If upperdelta == 2, then the difference is greater than 1
	// and we know that rounding up falls within the bound.
	var upperdelta uint8

	// Now we can figure out the minimum number of digits required.
	// Walk along until d has distinguished itself from upper and lower.
	for ui := 0; ; ui++ {
		// lower, d, and upper may have the decimal points at different
		// places. In this case upper is the longest, so we iterate from
		// ui==0 and start li and mi at (possibly) -1.
		mi := ui - upper.dp + d.dp
		if mi >= d.nd {
			break
		}
		li := ui - upper.dp + lower.dp
		l := byte('0') // lower digit
		if li >= 0 && li < lower.nd {
			l = lower.d[li]
		}
		m := byte('0') // middle digit
		if mi >= 0 {
			m = d.d[mi]
		}
		u := byte('0') // upper digit
		if ui < upper.nd {
			u = upper.d[ui]
		}

		// Okay to round down (truncate) if lower has a different digit
		// or if lower is inclusive and is exactly the result of rounding
		// down (i.e., and we have reached the final digit of lower).
		okdown := l != m || inclusive && li+1 == lower.nd

		switch {
		case upperdelta == 0 && m+1 < u:
			// Example:
			// m = 12345xxx
			// u = 12347xxx
			upperdelta = 2
		case upperdelta == 0 && m != u:
			// Example:
			// m = 12345xxx
			// u = 12346xxx
			upperdelta = 1
		case upperdelta == 1 && (m != '9' || u != '0'):
			// Example:
			// m = 1234598x
			// u = 1234600x
			upperdelta = 2
		}
		// Okay to round up if upper has a different digit and either upper
		// is inclusive or upper is bigger than the result of rounding up.
		okup := upperdelta > 0 && (inclusive || upperdelta > 1 || ui+1 < upper.nd)

		// If it's okay to do either, then round to the nearest one.
		// If it's okay to do only one, do it.
		switch {
		case okdown && okup:
			d.Round(mi + 1)
			return
		case okdown:
			d.RoundDown(mi + 1)
			return
		case okup:
			d.RoundUp(mi + 1)
			return
		}
	}
}

type decimalSlice struct {
	d      []byte
	nd, dp int
	neg    bool
}

// %e: -d.ddddde±dd
func fmtE(dst []byte, neg bool, d decimalSlice, prec int, fmt byte) []byte {
	// sign
	if neg {
		dst = append(dst, '-')
	}

	// first digit
	ch := byte('0')
	if d.nd != 0 {
		ch = d.d[0]
	}
	dst = append(dst, ch)

	// .moredigits
	if prec > 0 {
		dst = append(dst, '.')
		i := 1
		m := min(d.nd, prec+1)
		if i < m {
			dst = append(dst, d.d[i:m]...)
			i = m
		}
		for ; i <= prec; i++ {
			dst = append(dst, '0')
		}
	}

	// e±
	dst = append(dst, fmt)
	exp := d.dp - 1
	if d.nd == 0 { // special case: 0 has exponent 0
		exp = 0
	}
	if exp < 0 {
		ch = '-'
		exp = -exp
	} else {
		ch = '+'
	}
	dst = append(dst, ch)

	// dd or ddd
	switch {
	case exp < 10:
		dst = append(dst, '0', byte(exp)+'0')
	case exp < 100:
		dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
	default:
		dst = append(dst, byte(exp/100)+'0', byte(exp/10)%10+'0', byte(exp%10)+'0')
	}

	return dst
}

// %f: -ddddddd.ddddd
func fmtF(dst []byte, neg bool, d decimalSlice, prec int) []byte {
	// sign
	if neg {
		dst = append(dst, '-')
	}

	// integer, padded with zeros as needed.
	if d.dp > 0 {
		m := min(d.nd, d.dp)
		dst = append(dst, d.d[:m]...)
		for ; m < d.dp; m++ {
			dst = append(dst, '0')
		}
	} else {
		dst = append(dst, '0')
	}

	// fraction
	if prec > 0 {
		dst = append(dst, '.')
		for i := 0; i < prec; i++ {
			ch := byte('0')
			if j := d.dp + i; 0 <= j && j < d.nd {
				ch = d.d[j]
			}
			dst = append(dst, ch)
		}
	}

	return dst
}

// %b: -ddddddddp±ddd
func fmtB(dst []byte, neg bool, mant uint64, exp int, flt *floatInfo) []byte {
	// sign
	if neg {
		dst = append(dst, '-')
	}

	// mantissa
	dst, _ = formatBits(dst, mant, 10, false, true)

	// p
	dst = append(dst, 'p')

	// ±exponent
	exp -= int(flt.mantbits)
	if exp >= 0 {
		dst = append(dst, '+')
	}
	dst, _ = formatBits(dst, uint64(exp), 10, exp < 0, true)

	return dst
}

// %x: -0x1.yyyyyyyyp±ddd or -0x0p+0. (y is hex digit, d is decimal digit)
func fmtX(dst []byte, prec int, fmt byte, neg bool, mant uint64, exp int, flt *floatInfo) []byte {
	if mant == 0 {
		exp = 0
	}

	// Shift digits so leading 1 (if any) is at bit 1<<60.
	mant <<= 60 - flt.mantbits
	for mant != 0 && mant&(1<<60) == 0 {
		mant <<= 1
		exp--
	}

	// Round if requested.
	if prec >= 0 && prec < 15 {
		shift := uint(prec * 4)
		extra := (mant << shift) & (1<<60 - 1)
		mant >>= 60 - shift
		if extra|(mant&1) > 1<<59 {
			mant++
		}
		mant <<= 60 - shift
		if mant&(1<<61) != 0 {
			// Wrapped around.
			mant >>= 1
			exp++
		}
	}

	hex := lowerhex
	if fmt == 'X' {
		hex = upperhex
	}

	// sign, 0x, leading digit
	if neg {
		dst = append(dst, '-')
	}
	dst = append(dst, '0', fmt, '0'+byte((mant>>60)&1))

	// .fraction
	mant <<= 4 // remove leading 0 or 1
	if prec < 0 && mant != 0 {
		dst = append(dst, '.')
		for mant != 0 {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	} else if prec > 0 {
		dst = append(dst, '.')
		for i := 0; i < prec; i++ {
			dst = append(dst, hex[(mant>>60)&15])
			mant <<= 4
		}
	}

	// p±
	ch := byte('P')
	if fmt == lower(fmt) {
		ch = 'p'
	}
	dst = append(dst, ch)
	if exp < 0 {
		ch = '-'
		exp = -exp
	} else {
		ch = '+'
	}
	dst = append(dst, ch)

	// dd or ddd or dddd
	switch {
	case exp < 100:
		dst = append(dst, byte(exp/10)+'0', byte(exp%10)+'0')
	case exp < 1000:
		dst = append(dst, byte(exp/100)+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	default:
		dst = append(dst, byte(exp/1000)+'0', byte(exp/100)%10+'0', byte((exp/10)%10)+'0', byte(exp%10)+'0')
	}

	return dst
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatBits computes the string representation of u in the given base.
// If neg is set, u is treated as negative int64 value. If append_ is
// set, the string is appended to dst and the resulting byte slice is
// returned as the first result value; otherwise the string is returned
// as the second result value.
//
func formatBits(dst []byte, u uint64, base int, neg, append_ bool) (d []byte, s string) {
	if base < 2 || base > len(digits) {
		panic("strconv: illegal AppendInt/FormatInt base")
	}
	// 2 <= base && base <= len(digits)

	var a [64 + 1]byte // +1 for sign of 64bit value in base 2
	i := len(a)

	if neg {
		u = -u
	}

	// convert bits
	// We use uint values where we can because those will
	// fit into a single register even on a 32bit machine.
	if base == 10 {
		// common case: use constants for / because
		// the compiler can optimize it into a multiply+shift

		if host32bit {
			// convert the lower digits using 32bit operations
			for u >= 1e9 {
				// Avoid using r = a%b in addition to q = a/b
				// since 64bit division and modulo operations
				// are calculated by runtime functions on 32bit machines.
				q := u / 1e9
				us := uint(u - q*1e9) // u % 1e9 fits into a uint
				for j := 4; j > 0; j-- {
					is := us % 100 * 2
					us /= 100
					i -= 2
					a[i+1] = smallsString[is+1]
					a[i+0] = smallsString[is+0]
				}

				// us < 10, since it contains the last digit
				// from the initial 9-digit us.
				i--
				a[i] = smallsString[us*2+1]

				u = q
			}
			// u < 1e9
		}

		// u guaranteed to fit into a uint
		us := uint(u)
		for us >= 100 {
			is := us % 100 * 2
			us /= 100
			i -= 2
			a[i+1] = smallsString[is+1]
			a[i+0] = smallsString[is+0]
		}

		// us < 100
		is := us * 2
		i--
		a[i] = smallsString[is+1]
		if us >= 10 {
			i--
			a[i] = smallsString[is]
		}

	} else if isPowerOfTwo(base) {
		// Use shifts and masks instead of / and %.
		// Base is a power of 2 and 2 <= base <= len(digits) where len(digits) is 36.
		// The largest power of 2 below or equal to 36 is 32, which is 1 << 5;
		// i.e., the largest possible shift count is 5. By &-ind that value with
		// the constant 7 we tell the compiler that the shift count is always
		// less than 8 which is smaller than any register width. This allows
		// the compiler to generate better code for the shift operation.
		shift := uint(bits.TrailingZeros(uint(base))) & 7
		b := uint64(base)
		m := uint(base) - 1 // == 1<<shift - 1
		for u >= b {
			i--
			a[i] = digits[uint(u)&m]
			u >>= shift
		}
		// u < base
		i--
		a[i] = digits[uint(u)]
	} else {
		// general case
		b := uint64(base)
		for u >= b {
			i--
			// Avoid using r = a%b in addition to q = a/b
			// since 64bit division and modulo operations
			// are calculated by runtime functions on 32bit machines.
			q := u / b
			a[i] = digits[uint(u-q*b)]
			u = q
		}
		// u < base
		i--
		a[i] = digits[uint(u)]
	}

	// add sign, if any
	if neg {
		i--
		a[i] = '-'
	}

	if append_ {
		d = append(dst, a[i:]...)
		return
	}
	s = string(a[i:])
	return
}

func isPowerOfTwo(x int) bool {
	return x&(x-1) == 0
}

// AssignComputeBounds sets f to the floating point value
// defined by mant, exp and precision given by flt. It returns
// lower, upper such that any number in the closed interval
// [lower, upper] is converted back to the same floating point number.
func (f *extFloat) AssignComputeBounds(mant uint64, exp int, neg bool, flt *floatInfo) (lower, upper extFloat) {
	f.mant = mant
	f.exp = exp - int(flt.mantbits)
	f.neg = neg
	if f.exp <= 0 && mant == (mant>>uint(-f.exp))<<uint(-f.exp) {
		// An exact integer
		f.mant >>= uint(-f.exp)
		f.exp = 0
		return *f, *f
	}
	expBiased := exp - flt.bias

	upper = extFloat{mant: 2*f.mant + 1, exp: f.exp - 1, neg: f.neg}
	if mant != 1<<flt.mantbits || expBiased == 1 {
		lower = extFloat{mant: 2*f.mant - 1, exp: f.exp - 1, neg: f.neg}
	} else {
		lower = extFloat{mant: 4*f.mant - 1, exp: f.exp - 2, neg: f.neg}
	}
	return
}

// ShortestDecimal stores in d the shortest decimal representation of f
// which belongs to the open interval (lower, upper), where f is supposed
// to lie. It returns false whenever the result is unsure. The implementation
// uses the Grisu3 algorithm.
func (f *extFloat) ShortestDecimal(d *decimalSlice, lower, upper *extFloat) bool {
	if f.mant == 0 {
		d.nd = 0
		d.dp = 0
		d.neg = f.neg
		return true
	}
	if f.exp == 0 && *lower == *f && *lower == *upper {
		// an exact integer.
		var buf [24]byte
		n := len(buf) - 1
		for v := f.mant; v > 0; {
			v1 := v / 10
			v -= 10 * v1
			buf[n] = byte(v + '0')
			n--
			v = v1
		}
		nd := len(buf) - n - 1
		for i := 0; i < nd; i++ {
			d.d[i] = buf[n+1+i]
		}
		d.nd, d.dp = nd, nd
		for d.nd > 0 && d.d[d.nd-1] == '0' {
			d.nd--
		}
		if d.nd == 0 {
			d.dp = 0
		}
		d.neg = f.neg
		return true
	}
	upper.Normalize()
	// Uniformize exponents.
	if f.exp > upper.exp {
		f.mant <<= uint(f.exp - upper.exp)
		f.exp = upper.exp
	}
	if lower.exp > upper.exp {
		lower.mant <<= uint(lower.exp - upper.exp)
		lower.exp = upper.exp
	}

	exp10 := frexp10Many(lower, f, upper)
	// Take a safety margin due to rounding in frexp10Many, but we lose precision.
	upper.mant++
	lower.mant--

	// The shortest representation of f is either rounded up or down, but
	// in any case, it is a truncation of upper.
	shift := uint(-upper.exp)
	integer := uint32(upper.mant >> shift)
	fraction := upper.mant - (uint64(integer) << shift)

	// How far we can go down from upper until the result is wrong.
	allowance := upper.mant - lower.mant
	// How far we should go to get a very precise result.
	targetDiff := upper.mant - f.mant

	// Count integral digits: there are at most 10.
	var integerDigits int
	for i, pow := 0, uint64(1); i < 20; i++ {
		if pow > uint64(integer) {
			integerDigits = i
			break
		}
		pow *= 10
	}
	for i := 0; i < integerDigits; i++ {
		pow := uint64pow10[integerDigits-i-1]
		digit := integer / uint32(pow)
		d.d[i] = byte(digit + '0')
		integer -= digit * uint32(pow)
		// evaluate whether we should stop.
		if currentDiff := uint64(integer)<<shift + fraction; currentDiff < allowance {
			d.nd = i + 1
			d.dp = integerDigits + exp10
			d.neg = f.neg
			// Sometimes allowance is so large the last digit might need to be
			// decremented to get closer to f.
			return adjustLastDigit(d, currentDiff, targetDiff, allowance, pow<<shift, 2)
		}
	}
	d.nd = integerDigits
	d.dp = d.nd + exp10
	d.neg = f.neg

	// Compute digits of the fractional part. At each step fraction does not
	// overflow. The choice of minExp implies that fraction is less than 2^60.
	var digit int
	multiplier := uint64(1)
	for {
		fraction *= 10
		multiplier *= 10
		digit = int(fraction >> shift)
		d.d[d.nd] = byte(digit + '0')
		d.nd++
		fraction -= uint64(digit) << shift
		if fraction < allowance*multiplier {
			// We are in the admissible range. Note that if allowance is about to
			// overflow, that is, allowance > 2^64/10, the condition is automatically
			// true due to the limited range of fraction.
			return adjustLastDigit(d,
				fraction, targetDiff*multiplier, allowance*multiplier,
				1<<shift, multiplier*2)
		}
	}
}

// FixedDecimal stores in d the first n significant digits
// of the decimal representation of f. It returns false
// if it cannot be sure of the answer.
func (f *extFloat) FixedDecimal(d *decimalSlice, n int) bool {
	if f.mant == 0 {
		d.nd = 0
		d.dp = 0
		d.neg = f.neg
		return true
	}
	if n == 0 {
		panic("strconv: internal error: extFloat.FixedDecimal called with n == 0")
	}
	// Multiply by an appropriate power of ten to have a reasonable
	// number to process.
	f.Normalize()
	exp10, _ := f.frexp10()

	shift := uint(-f.exp)
	integer := uint32(f.mant >> shift)
	fraction := f.mant - (uint64(integer) << shift)
	ε := uint64(1) // ε is the uncertainty we have on the mantissa of f.

	// Write exactly n digits to d.
	needed := n        // how many digits are left to write.
	integerDigits := 0 // the number of decimal digits of integer.
	pow10 := uint64(1) // the power of ten by which f was scaled.
	for i, pow := 0, uint64(1); i < 20; i++ {
		if pow > uint64(integer) {
			integerDigits = i
			break
		}
		pow *= 10
	}
	rest := integer
	if integerDigits > needed {
		// the integral part is already large, trim the last digits.
		pow10 = uint64pow10[integerDigits-needed]
		integer /= uint32(pow10)
		rest -= integer * uint32(pow10)
	} else {
		rest = 0
	}

	// Write the digits of integer: the digits of rest are omitted.
	var buf [32]byte
	pos := len(buf)
	for v := integer; v > 0; {
		v1 := v / 10
		v -= 10 * v1
		pos--
		buf[pos] = byte(v + '0')
		v = v1
	}
	for i := pos; i < len(buf); i++ {
		d.d[i-pos] = buf[i]
	}
	nd := len(buf) - pos
	d.nd = nd
	d.dp = integerDigits + exp10
	needed -= nd

	if needed > 0 {
		if rest != 0 || pow10 != 1 {
			panic("strconv: internal error, rest != 0 but needed > 0")
		}
		// Emit digits for the fractional part. Each time, 10*fraction
		// fits in a uint64 without overflow.
		for needed > 0 {
			fraction *= 10
			ε *= 10 // the uncertainty scales as we multiply by ten.
			if 2*ε > 1<<shift {
				// the error is so large it could modify which digit to write, abort.
				return false
			}
			digit := fraction >> shift
			d.d[nd] = byte(digit + '0')
			fraction -= digit << shift
			nd++
			needed--
		}
		d.nd = nd
	}

	// We have written a truncation of f (a numerator / 10^d.dp). The remaining part
	// can be interpreted as a small number (< 1) to be added to the last digit of the
	// numerator.
	//
	// If rest > 0, the amount is:
	//    (rest<<shift | fraction) / (pow10 << shift)
	//    fraction being known with a ±ε uncertainty.
	//    The fact that n > 0 guarantees that pow10 << shift does not overflow a uint64.
	//
	// If rest = 0, pow10 == 1 and the amount is
	//    fraction / (1 << shift)
	//    fraction being known with a ±ε uncertainty.
	//
	// We pass this information to the rounding routine for adjustment.

	ok := adjustLastDigitFixed(d, uint64(rest)<<shift|fraction, pow10, shift, ε)
	if !ok {
		return false
	}
	// Trim trailing zeros.
	for i := d.nd - 1; i >= 0; i-- {
		if d.d[i] != '0' {
			d.nd = i + 1
			break
		}
	}
	return true
}

// Assign v to a.
func (a *decimal) Assign(v uint64) {
	var buf [24]byte

	// Write reversed decimal in buf.
	n := 0
	for v > 0 {
		v1 := v / 10
		v -= 10 * v1
		buf[n] = byte(v + '0')
		n++
		v = v1
	}

	// Reverse again to produce forward decimal in a.d.
	a.nd = 0
	for n--; n >= 0; n-- {
		a.d[a.nd] = buf[n]
		a.nd++
	}
	a.dp = a.nd
	trim(a)
}

// Binary shift left (k > 0) or right (k < 0).
func (a *decimal) Shift(k int) {
	switch {
	case a.nd == 0:
		// nothing to do: a == 0
	case k > 0:
		for k > maxShift {
			leftShift(a, maxShift)
			k -= maxShift
		}
		leftShift(a, uint(k))
	case k < 0:
		for k < -maxShift {
			rightShift(a, maxShift)
			k += maxShift
		}
		rightShift(a, uint(-k))
	}
}

// Round a to nd digits (or fewer).
// If nd is zero, it means we're rounding
// just to the left of the digits, as in
// 0.09 -> 0.1.
func (a *decimal) Round(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}
	if shouldRoundUp(a, nd) {
		a.RoundUp(nd)
	} else {
		a.RoundDown(nd)
	}
}

// Round a down to nd digits (or fewer).
func (a *decimal) RoundDown(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}
	a.nd = nd
	trim(a)
}

// Round a up to nd digits (or fewer).
func (a *decimal) RoundUp(nd int) {
	if nd < 0 || nd >= a.nd {
		return
	}

	// round up
	for i := nd - 1; i >= 0; i-- {
		c := a.d[i]
		if c < '9' { // can stop after this digit
			a.d[i]++
			a.nd = i + 1
			return
		}
	}

	// Number is all 9s.
	// Change to single 1 with adjusted decimal point.
	a.d[0] = '1'
	a.nd = 1
	a.dp++
}

// lower(c) is a lower-case letter if and only if
// c is either that lower-case letter or the equivalent upper-case letter.
// Instead of writing c == 'x' || c == 'X' one can write lower(c) == 'x'.
// Note that lower of non-letters can produce other non-letters.
func lower(c byte) byte {
	return c | ('x' - 'X')
}

// Normalize normalizes f so that the highest bit of the mantissa is
// set, and returns the number by which the mantissa was left-shifted.
func (f *extFloat) Normalize() uint {
	// bits.LeadingZeros64 would return 64
	if f.mant == 0 {
		return 0
	}
	shift := bits.LeadingZeros64(f.mant)
	f.mant <<= uint(shift)
	f.exp -= shift
	return uint(shift)
}

// frexp10Many applies a common shift by a power of ten to a, b, c.
func frexp10Many(a, b, c *extFloat) (exp10 int) {
	exp10, i := c.frexp10()
	a.Multiply(powersOfTen[i])
	b.Multiply(powersOfTen[i])
	return
}

// adjustLastDigitFixed assumes d contains the representation of the integral part
// of some number, whose fractional part is num / (den << shift). The numerator
// num is only known up to an uncertainty of size ε, assumed to be less than
// (den << shift)/2.
//
// It will increase the last digit by one to account for correct rounding, typically
// when the fractional part is greater than 1/2, and will return false if ε is such
// that no correct answer can be given.
func adjustLastDigitFixed(d *decimalSlice, num, den uint64, shift uint, ε uint64) bool {
	if num > den<<shift {
		panic("strconv: num > den<<shift in adjustLastDigitFixed")
	}
	if 2*ε > den<<shift {
		panic("strconv: ε > (den<<shift)/2")
	}
	if 2*(num+ε) < den<<shift {
		return true
	}
	if 2*(num-ε) > den<<shift {
		// increment d by 1.
		i := d.nd - 1
		for ; i >= 0; i-- {
			if d.d[i] == '9' {
				d.nd--
			} else {
				break
			}
		}
		if i < 0 {
			d.d[0] = '1'
			d.nd = 1
			d.dp++
		} else {
			d.d[i]++
		}
		return true
	}
	return false
}

// Frexp10 is an analogue of math.Frexp for decimal powers. It scales
// f by an approximate power of ten 10^-exp, and returns exp10, so
// that f*10^exp10 has the same value as the old f, up to an ulp,
// as well as the index of 10^-exp in the powersOfTen table.
func (f *extFloat) frexp10() (exp10, index int) {
	// The constants expMin and expMax constrain the final value of the
	// binary exponent of f. We want a small integral part in the result
	// because finding digits of an integer requires divisions, whereas
	// digits of the fractional part can be found by repeatedly multiplying
	// by 10.
	const expMin = -60
	const expMax = -32
	// Find power of ten such that x * 10^n has a binary exponent
	// between expMin and expMax.
	approxExp10 := ((expMin+expMax)/2 - f.exp) * 28 / 93 // log(10)/log(2) is close to 93/28.
	i := (approxExp10 - firstPowerOfTen) / stepPowerOfTen
Loop:
	for {
		exp := f.exp + powersOfTen[i].exp + 64
		switch {
		case exp < expMin:
			i++
		case exp > expMax:
			i--
		default:
			break Loop
		}
	}
	// Apply the desired decimal shift on f. It will have exponent
	// in the desired range. This is multiplication by 10^-exp10.
	f.Multiply(powersOfTen[i])

	return -(firstPowerOfTen + i*stepPowerOfTen), i
}

// trim trailing zeros from number.
// (They are meaningless; the decimal point is tracked
// independent of the number of digits.)
func trim(a *decimal) {
	for a.nd > 0 && a.d[a.nd-1] == '0' {
		a.nd--
	}
	if a.nd == 0 {
		a.dp = 0
	}
}

const uintSize = 32 << (^uint(0) >> 63)
const maxShift = uintSize - 4

// Binary shift right (/ 2) by k bits.  k <= maxShift to avoid overflow.
func rightShift(a *decimal, k uint) {
	r := 0 // read pointer
	w := 0 // write pointer

	// Pick up enough leading digits to cover first shift.
	var n uint
	for ; n>>k == 0; r++ {
		if r >= a.nd {
			if n == 0 {
				// a == 0; shouldn't get here, but handle anyway.
				a.nd = 0
				return
			}
			for n>>k == 0 {
				n = n * 10
				r++
			}
			break
		}
		c := uint(a.d[r])
		n = n*10 + c - '0'
	}
	a.dp -= r - 1

	var mask uint = (1 << k) - 1

	// Pick up a digit, put down a digit.
	for ; r < a.nd; r++ {
		c := uint(a.d[r])
		dig := n >> k
		n &= mask
		a.d[w] = byte(dig + '0')
		w++
		n = n*10 + c - '0'
	}

	// Put down extra digits.
	for n > 0 {
		dig := n >> k
		n &= mask
		if w < len(a.d) {
			a.d[w] = byte(dig + '0')
			w++
		} else if dig > 0 {
			a.trunc = true
		}
		n = n * 10
	}

	a.nd = w
	trim(a)
}

// Binary shift left (* 2) by k bits.  k <= maxShift to avoid overflow.
func leftShift(a *decimal, k uint) {
	delta := leftcheats[k].delta
	if prefixIsLessThan(a.d[0:a.nd], leftcheats[k].cutoff) {
		delta--
	}

	r := a.nd         // read index
	w := a.nd + delta // write index

	// Pick up a digit, put down a digit.
	var n uint
	for r--; r >= 0; r-- {
		n += (uint(a.d[r]) - '0') << k
		quo := n / 10
		rem := n - 10*quo
		w--
		if w < len(a.d) {
			a.d[w] = byte(rem + '0')
		} else if rem != 0 {
			a.trunc = true
		}
		n = quo
	}

	// Put down extra digits.
	for n > 0 {
		quo := n / 10
		rem := n - 10*quo
		w--
		if w < len(a.d) {
			a.d[w] = byte(rem + '0')
		} else if rem != 0 {
			a.trunc = true
		}
		n = quo
	}

	a.nd += delta
	if a.nd >= len(a.d) {
		a.nd = len(a.d)
	}
	a.dp += delta
	trim(a)
}

// adjustLastDigit modifies d = x-currentDiff*ε, to get closest to
// d = x-targetDiff*ε, without becoming smaller than x-maxDiff*ε.
// It assumes that a decimal digit is worth ulpDecimal*ε, and that
// all data is known with an error estimate of ulpBinary*ε.
func adjustLastDigit(d *decimalSlice, currentDiff, targetDiff, maxDiff, ulpDecimal, ulpBinary uint64) bool {
	if ulpDecimal < 2*ulpBinary {
		// Approximation is too wide.
		return false
	}
	for currentDiff+ulpDecimal/2+ulpBinary < targetDiff {
		d.d[d.nd-1]--
		currentDiff += ulpDecimal
	}
	if currentDiff+ulpDecimal <= targetDiff+ulpDecimal/2+ulpBinary {
		// we have two choices, and don't know what to do.
		return false
	}
	if currentDiff < ulpBinary || currentDiff > maxDiff-ulpBinary {
		// we went too far
		return false
	}
	if d.nd == 1 && d.d[0] == '0' {
		// the number has actually reached zero.
		d.nd = 0
		d.dp = 0
	}
	return true
}

// If we chop a at nd digits, should we round up?
func shouldRoundUp(a *decimal, nd int) bool {
	if nd < 0 || nd >= a.nd {
		return false
	}
	if a.d[nd] == '5' && nd+1 == a.nd { // exactly halfway - round to even
		// if we truncated, a little higher than what's recorded - always round up
		if a.trunc {
			return true
		}
		return nd > 0 && (a.d[nd-1]-'0')%2 != 0
	}
	// not halfway - digit tells all
	return a.d[nd] >= '5'
}

// Multiply sets f to the product f*g: the result is correctly rounded,
// but not normalized.
func (f *extFloat) Multiply(g extFloat) {
	hi, lo := bits.Mul64(f.mant, g.mant)
	// Round up.
	f.mant = hi + (lo >> 63)
	f.exp = f.exp + g.exp + 64
}
