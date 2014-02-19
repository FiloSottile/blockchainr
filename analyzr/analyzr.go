package main

import (
	"encoding/hex"
	"encoding/json"
	"github.com/FiloSottile/blockchainr"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwire"
	// "github.com/davecgh/go-spew/spew"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
)

var exported_blocks map[string]string

func grabTxIn(blkid int64, tx_prefix string, txin_n int) *btcwire.TxIn {
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
		log.Println(blkid, txsha.String(), txin_n)
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

	balanceCache := make(map[string][]byte)

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
			sigStr, remaining, err := blockchainr.PopData(txin.SignatureScript)
			if err != nil {
				log.Fatalln("PopData failed:", err)
			}
			pkStr, _, err := blockchainr.PopData(remaining)
			if err != nil {
				log.Println("The second PopData failed - probably a pay-to-PubKey:", err)
				continue
			}

			_ = sigStr

			// log.Println(spew.Sdump(sigStr), spew.Sdump(pkStr))

			// pubKey, err := btcec.ParsePubKey(pkStr, btcec.S256())
			// if err != nil {
			// 	log.Fatalln("ParsePubKey failed:", err)
			// }
			// signature, err := btcec.ParseSignature(sigStr, btcec.S256())
			// if err != nil {
			// 	log.Fatalln("ParseSignature failed:", err)
			// }

			aPubKey, err := btcutil.NewAddressPubKey(pkStr, btcwire.MainNet)
			address := aPubKey.EncodeAddress()

			balance := balanceCache[address]
			if balance == nil {
				response, err := http.Get("https://blockchain.info/q/addressbalance/" + address)
				if err != nil {
					log.Fatalln("Get failed:", err)
				}
				defer response.Body.Close()
				balance, err = ioutil.ReadAll(response.Body)
				if err != nil {
					log.Fatalln("ReadAll failed:", err)
				}
				balanceCache[address] = balance
			}

			log.Println(address, string(balance))
		}
	}
}
