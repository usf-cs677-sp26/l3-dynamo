package main

import (
	"crypto/md5"
	"file-transfer/messages"
	"file-transfer/util"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func put(msgHandler *messages.MessageHandler, fileName string) int {
	fmt.Println("PUT", fileName)

	// Get file size and make sure it exists
	info, err := os.Stat(fileName)
	if err != nil {
		log.Fatalln(err)
	}

	// Tell the server we want to store this file
	_, fname := filepath.Split(fileName)
	msgHandler.SendStorageRequest(fname, uint64(info.Size()))
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return 1
	}

	file, _ := os.Open(fileName)
	md5 := md5.New()
	w := io.MultiWriter(msgHandler, md5)

	/* Proper error checking and handling is required, based on the same principle as in server. */
	bytesRead, err := io.CopyN(w, file, info.Size()) // Checksum and transfer file at same time
	file.Close()

	if err != nil {
		log.Printf("Error sending file data: %v", err)
		return 1
	}
	if bytesRead != info.Size() {
		log.Printf("Incomplete transfer: expected %d bytes, sent %d bytes", info.Size(), bytesRead)
		return 1
	}

	checksum := md5.Sum(nil)
	msgHandler.SendChecksumVerification(checksum)
	if ok, _ := msgHandler.ReceiveResponse(); !ok {
		return 1
	}

	fmt.Println("Storage complete!")
	return 0
}

func get(msgHandler *messages.MessageHandler, fileName string) int {
	fmt.Println("GET", fileName)

	_, fname := filepath.Split(fileName)

	// Create the file in write-only mode, but fail if it already exists
	file, err := os.OpenFile(fname, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		log.Println(err)
		return 1
	}

	msgHandler.SendRetrievalRequest(fileName)
	ok, _, size := msgHandler.ReceiveRetrievalResponse()
	if !ok {
		return 1
	}

	md5 := md5.New()
	w := io.MultiWriter(file, md5)
	bytesWritten, err := io.CopyN(w, msgHandler, int64(size))
	file.Close()
	if err != nil {
		log.Printf("Error receiving file data: %v", err)
		return 1
	}
	if bytesWritten != int64(size) {
		log.Printf("Incomplete transfer: expected %d bytes, stored %d bytes", size, bytesWritten)
		return 1
	}

	clientCheck := md5.Sum(nil)
	checkMsg, _ := msgHandler.Receive()
	serverCheck := checkMsg.GetChecksum().Checksum

	if util.VerifyChecksum(serverCheck, clientCheck) {
		log.Println("Successfully retrieved file.")
	} else {
		log.Println("FAILED to retrieve file. Invalid checksum.")
	}

	return 0
}

func main() {
	if len(os.Args) < 4 {
		fmt.Printf("Not enough arguments. Usage: %s server:port put|get file-name [download-dir]\n", os.Args[0])
		os.Exit(1)
	}

	host := os.Args[1]
	conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatalln(err.Error())
		return
	}
	msgHandler := messages.NewMessageHandler(conn)
	defer conn.Close()

	action := strings.ToLower(os.Args[2])
	if action != "put" && action != "get" {
		log.Fatalln("Invalid action", action)
	}

	fileName := os.Args[3]

	dir := "."
	if len(os.Args) >= 5 {
		dir = os.Args[4]
	}
	if err := os.Chdir(dir); err != nil {
		log.Fatalln(err)
	}

	if action == "put" {
		os.Exit(put(msgHandler, fileName))
	} else if action == "get" {
		os.Exit(get(msgHandler, fileName))
	}
}
