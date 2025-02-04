package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/edgelesssys/ego/attestation"
	"github.com/edgelesssys/ego/attestation/tcbstatus"
	"github.com/edgelesssys/ego/eclient"
)

func main() {
	signerArg := flag.String("s", "", "signer ID")
	serverAddr := flag.String("a", "localhost:8080", "server address")
	isSim := flag.String("m", "0", "is simulation mode")
	flag.Parse()

	// get signer command line argument
	signer, err := hex.DecodeString(*signerArg)
	if err != nil {
		panic(err)
	}
	if len(signer) == 0 {
		flag.Usage()
		return
	}

	url := "https://" + *serverAddr

	// Get server certificate and its report. Skip TLS certificate verification because
	// the certificate is self-signed and we will verify it using the report instead.
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	certBytes := httpGet(tlsConfig, url+"/cert")
	reportBytes := httpGet(tlsConfig, url+"/report")

	if *isSim == "1" {
		os.Setenv("OE_SIMULATION", "1")
		if err := verifySimReport(reportBytes, certBytes, signer); err != nil {
			panic(err)
		}
	} else {
		if err := verifyReport(reportBytes, certBytes, signer); err != nil {
			panic(err)
		}
	}

	// Create a TLS config that uses the server certificate as root
	// CA so that future connections to the server can be verified.
	cert, _ := x509.ParseCertificate(certBytes)
	tlsConfig = &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "localhost"}
	tlsConfig.RootCAs.AddCert(cert)

	httpGet(tlsConfig, url+"/secret?s=mySecret")
	fmt.Println("Sent secret over attested TLS channel.")
}

func verifySimReport(reportBytes, certBytes, signer []byte) error {

	hash := sha256.Sum256(certBytes)
	if !bytes.Equal(reportBytes, hash[:]) {
		return errors.New("report data does not match the certificate's hash")
	}

	return nil
}

func verifyReport(reportBytes, certBytes, signer []byte) error {
	
	report, err := eclient.VerifyRemoteReport(reportBytes)
	if err == attestation.ErrTCBLevelInvalid {
		fmt.Printf("Warning: TCB level is invalid: %v\n%v\n", report.TCBStatus, tcbstatus.Explain(report.TCBStatus))
		fmt.Println("We'll ignore this issue in this sample. For an app that should run in production, you must decide which of the different TCBStatus values are acceptable for you to continue.")
	} else if err != nil {
		return err
	}

	hash := sha256.Sum256(certBytes)
	if !bytes.Equal(report.Data[:len(hash)], hash[:]) {
		return errors.New("report data does not match the certificate's hash")
	}

	// You can either verify the UniqueID or the tuple (SignerID, ProductID, SecurityVersion, Debug).

	if report.SecurityVersion < 2 {
		return errors.New("invalid security version")
	}
	if binary.LittleEndian.Uint16(report.ProductID) != 1234 {
		return errors.New("invalid product")
	}
	if !bytes.Equal(report.SignerID, signer) {
		return errors.New("invalid signer")
	}

	// For production, you must also verify that report.Debug == false

	return nil
}

func httpGet(tlsConfig *tls.Config, url string) []byte {
	client := http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	resp, err := client.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(resp.Status)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return body
}
