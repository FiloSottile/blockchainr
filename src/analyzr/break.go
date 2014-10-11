package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"log"
	"math/big"
	"reflect"

	"github.com/conformal/btcdb"
	"github.com/conformal/btcec"
	"github.com/conformal/btcnet"
	"github.com/conformal/btcutil"
)

// from crypto/ecdsa
func hashToInt(hash []byte, c elliptic.Curve) *big.Int {
	orderBits := c.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}

func recoverKey(sigA, sigB *btcec.Signature, hashA, hashB []byte, pubKey *btcec.PublicKey) *btcec.PrivateKey {
	// Sanity checks
	if sigA.R.Cmp(sigB.R) != 0 {
		log.Println("Different R!")
		return nil
	}
	if !ecdsa.Verify(pubKey.ToECDSA(), hashA, sigA.R, sigA.S) {
		log.Println("A fails to verify!")
		return nil
	}
	if !ecdsa.Verify(pubKey.ToECDSA(), hashB, sigB.R, sigB.S) {
		log.Println("B fails to verify!")
		return nil
	}
	if !reflect.DeepEqual(pubKey.Curve, btcec.S256()) {
		log.Println("What the curve?!")
		return nil
	}

	c := btcec.S256()

	N := c.Params().N
	zA := hashToInt(hashA, c)
	zB := hashToInt(hashB, c)

	sDiffInv := new(big.Int).Sub(sigA.S, sigB.S)
	sDiffInv.Mod(sDiffInv, N)
	sDiffInv.ModInverse(sDiffInv, N)

	zDiff := new(big.Int).Sub(zA, zB)
	zDiff.Mod(zDiff, N)

	k := new(big.Int).Mul(zDiff, sDiffInv)
	k.Mod(k, N)

	rInv := new(big.Int).ModInverse(sigA.R, N)

	D := new(big.Int)
	D.Mul(sigA.S, k)
	D.Sub(D, zA)
	D.Mul(D, rInv)
	D.Mod(D, N)

	x, y := c.ScalarBaseMult(D.Bytes())
	if pubKey.X.Cmp(x) != 0 {
		log.Println("X!")
		return nil
	}
	if pubKey.Y.Cmp(y) != 0 {
		log.Println("Y!")
		return nil
	}

	return &btcec.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: c,
			X:     x,
			Y:     y,
		},
		D: D,
	}
}

func doTheMagic(targets map[[2]string][]*rData, db btcdb.Db) {
	for _, target := range targets {
		if len(target) < 2 {
			// The r value was reused across different addresses
			// TODO: also this information would be interesting to graph

			for _, rd := range target {
				printLine(rd)
			}

			continue
		}

		a := target[0]
		b := target[1]

		log.Printf("[%v]\n", a.address)
		log.Printf("Repeated r value: %v (%v times)\n", a.r, len(target))

		privKey := recoverKey(a.signature, b.signature, a.hash, b.hash, a.pubKey)
		if privKey == nil {
			log.Print("recoverKey error\n\n")
			continue
		}

		wif, err := btcutil.NewWIF(privKey, &btcnet.MainNetParams, a.compressed)
		if err != nil {
			log.Printf("NewWIF error: %v\n\n", err)
			continue
		}

		for _, rd := range target {
			rd.wif = wif
			printLine(rd)
		}

		log.Printf("%v\n\n", wif.String())
	}
}
