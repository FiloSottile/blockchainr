package main

import (
	"encoding/hex"
	"encoding/json"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	// "github.com/davecgh/go-spew/spew"
	"github.com/FiloSottile/blockchainr"
	"io/ioutil"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
)

var exported_blocks map[string]string

func grabTxIn(blkid int64, tx_prefix string, txin_n int) *btcwire.TxIn {
	log.Println(blkid, tx_prefix, txin_n)

	hex_block := exported_blocks[strconv.FormatInt(blkid, 10)]
	if hex_block == "" {
		log.Fatalln("There is no data for the block", blkid)
	}
	block_data, err := hex.DecodeString(hex_block)
	if err != nil {
		log.Fatalln("hex DecodeString failed:", err)
	}
	block, err := btcutil.NewBlockFromBytes(block_data)
	if err != nil {
		log.Fatalln("NewBlockFromBytes failed:", err)
	}

	mblk := block.MsgBlock()
	var txin *btcwire.TxIn
	for _, tx := range mblk.Transactions {
		txsha, err := tx.TxSha()
		if err != nil {
			log.Fatalln("TxSha failed:", err)
		}
		if tx_prefix != txsha.String()[:5] {
			continue
		}
		if len(tx.TxIn) <= txin_n {
			// log.Println(txsha.String(), spew.Sdump(tx.TxIn))
			log.Fatalln("There are not enough TxIn")
		}
		txin = tx.TxIn[txin_n]
	}
	if txin == nil {
		log.Fatalln("Transaction not found")
	}

	return txin
}

func main() {
	if len(os.Args) < 3 {
		log.Fatalln("usage: analyzr blockchainr.json blocks.json")
	}
	blockchainrFile, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalln("failed to read blockchainr.json:", err)
	}
	blocksFile, err := ioutil.ReadFile(os.Args[2])
	if err != nil {
		log.Fatalln("failed to read blocks.json:", err)
	}

	exported_blocks = make(map[string]string)
	err = json.Unmarshal(blocksFile, &exported_blocks)
	if err != nil {
		log.Fatalln("Unmarshal error:", err)
	}
	duplicates := make(map[string][]string)
	err = json.Unmarshal(blockchainrFile, &duplicates)
	if err != nil {
		log.Fatalln("Unmarshal error:", err)
	}

	for key, value := range duplicates {
		R, res := new(big.Int).SetString(key, 10)
		if res != true {
			log.Fatalln("SetString error:", err)
		}
		log.Println(R)

		for i := 0; i < len(value); i++ {
			p := strings.Split(value[i], ":")
			if len(p) != 3 {
				log.Fatalln("The index is the wrong length")
			}
			blkid, err := strconv.ParseInt(p[0], 10, 64)
			if err != nil {
				log.Fatalln("ParseInt error:", err)
			}
			tx_prefix := p[1]
			txin_n, err := strconv.Atoi(p[2])
			if err != nil {
				log.Fatalln("Atoi error:", err)
			}

			txin := grabTxIn(blkid, tx_prefix, txin_n)
			sig, err := blockchainr.PopData(txin.SignatureScript)
			if err != nil {
				log.Fatalln("PopData failed:", err)
			}

			_ = sig
		}
	}
}
