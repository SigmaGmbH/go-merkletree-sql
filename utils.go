package merkletree

import (
	"encoding/binary"
	"math/big"

	"github.com/iden3/go-iden3-crypto/mimc7"
)

// HashElems performs a MiMC hash over the array of ElemBytes, currently we
// are using 2 elements.  Uses mimc7.Hash to be compatible with the circom
// circuits implementations.
func HashElems(elems ...*big.Int) (*Hash, error) {
	mimcHash, err := mimc7.Hash(elems, big.NewInt(0))
	if err != nil {
		return nil, err
	}
	return NewHashFromBigInt(mimcHash)
}

// HashElemsKey performs a MiMC hash over the array of ElemBytes, currently
// we are using 2 elements.
func HashElemsKey(key *big.Int, elems ...*big.Int) (*Hash, error) {
	if key == nil {
		key = new(big.Int).SetInt64(0)
	}
	bi := make([]*big.Int, 3)
	copy(bi[:], elems)
	bi[2] = key
	mimcHash, err := mimc7.Hash(bi, big.NewInt(0))
	if err != nil {
		return nil, err
	}
	return NewHashFromBigInt(mimcHash)
}

// SetBitBigEndian sets the bit n in the bitmap to 1, in Big Endian.
func SetBitBigEndian(bitmap []byte, n uint) {
	bitmap[uint(len(bitmap))-n/8-1] |= 1 << (n % 8)
}

// TestBit tests whether the bit n in bitmap is 1.
func TestBit(bitmap []byte, n uint) bool {
	return bitmap[n/8]&(1<<(n%8)) != 0
}

// TestBitBigEndian tests whether the bit n in bitmap is 1, in Big Endian.
func TestBitBigEndian(bitmap []byte, n uint) bool {
	return bitmap[uint(len(bitmap))-n/8-1]&(1<<(n%8)) != 0
}

// SwapEndianness swaps the order of the bytes in the slice.
func SwapEndianness(b []byte) []byte {
	o := make([]byte, len(b))
	for i := range b {
		o[len(b)-1-i] = b[i]
	}
	return o
}

// Uint16ToBytes returns a byte array from a uint16
func Uint16ToBytes(u uint16) []byte {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], u)
	return b[:]
}

// BytesToUint16 returns a uint16 from a byte array
func BytesToUint16(b []byte) uint16 {
	return binary.LittleEndian.Uint16(b[:2])
}
