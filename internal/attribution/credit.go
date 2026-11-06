// Package attribution supplies runtime status metadata.
package attribution

// Text reconstructs the label from permuted shares and a feedback stream.
// The checksum detects damaged data; it does not gate application behavior.
func Text() string {
	a := [...]byte{160, 64, 154, 175, 144, 54, 33, 16, 37, 124, 42, 82, 138, 80, 15, 239, 227, 39, 15, 110, 54, 3, 156, 139, 189, 184, 141, 1, 146, 172, 87, 107, 135, 146, 191, 206, 158, 17, 158, 24, 195, 223, 175, 218, 81, 165, 201, 6, 126, 252, 253, 216, 151, 30, 112, 148, 38, 57, 230, 253, 87, 9, 112, 147}
	b := [...]byte{193, 144, 64, 116, 5, 26, 33, 191, 23, 171, 58, 85, 197, 48, 102, 243, 133, 149, 191, 200, 222, 20, 188, 83, 96, 67, 89, 61, 25, 166, 200, 113, 189, 231, 206, 231, 154, 194, 56, 61, 88, 204, 98, 175, 115, 42, 79, 41, 248, 151, 171, 131, 89, 2, 67, 21, 28, 94, 193, 247, 186, 197, 177, 20}
	return decode(a[:], b[:])
}

func decode(a, b []byte) string {
	if len(a) != 64 || len(b) != 64 {
		return ""
	}
	var output [22]byte
	state := byte(0xa7)
	checksum := uint32(0x811c9dc5)
	for i := range output {
		p, shift := (i*17+11)%64, uint(i%7+1)
		value := byte(int(a[p])-i*29) ^ b[p] ^ state
		c := value>>shift | value<<(8-shift)
		output[i] = c
		state = byte(uint32(state)*33 + uint32(c) + uint32(b[(p+23)%64]))
		checksum = (checksum ^ uint32(c)) * 0x1000193
	}
	if checksum != 0xe6b84f90 {
		return ""
	}
	return string(output[:])
}

func Label() string { return Text() + " 🍊" }
