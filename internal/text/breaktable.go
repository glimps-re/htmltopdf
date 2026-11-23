package text

// The table is derived from testdata/chrome_breaks.txt and checked against it
// bit for bit by TestBreakTableIsDerivedFromChromeData.

// asciiBreak[c1] has bit c2 set when Chrome offers a line-break opportunity
// between two tailored characters of classes c1 and c2 (see classOf).
var asciiBreak = [numClasses]uint64{
	0x0000000000, // classLetter
	0x0000000000, // classDigit
	0x0ffffffffb, // classSpace
	0x0000000000, // classLatin
	0x0104200808, // punctuation !
	0x0104200800, // punctuation "
	0x0104200800, // punctuation #
	0x0000000000, // punctuation $
	0x0104200800, // punctuation %
	0x0104200800, // punctuation &
	0x0000000000, // punctuation '
	0x0000000000, // punctuation (
	0x0104200800, // punctuation )
	0x0104200800, // punctuation *
	0x0104200800, // punctuation +
	0x0104200800, // punctuation ,
	0x0beee16f69, // punctuation -
	0x0104200800, // punctuation .
	0x0000000008, // punctuation /
	0x0104200800, // punctuation :
	0x0104200800, // punctuation ;
	0x0000000000, // punctuation <
	0x0104200800, // punctuation =
	0x0104200800, // punctuation >
	0x0beee16bcb, // punctuation ?
	0x0000000000, // punctuation @
	0x0000000000, // punctuation [
	0x0104200800, // punctuation \
	0x0104200800, // punctuation ]
	0x0000000000, // punctuation ^
	0x0000000000, // punctuation _
	0x0000000000, // punctuation `
	0x0000000000, // punctuation {
	0x0104200808, // punctuation |
	0x0104200808, // punctuation }
	0x0104200800, // punctuation ~
}
