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
	"syscall"
)

func hasEnoughSpace(fileName string, required int64) (bool, error) {
	dir := filepath.Dir(fileName)
	var stat syscall.Statfs_t
	err := syscall.Statfs(dir, &stat)
	if err != nil {
		return false, err
	}
	// Available blocks * block size = available bytes
	available := int64(stat.Bavail) * int64(stat.Bsize)
	return available >= required, nil
}

func handleStorage(msgHandler *messages.MessageHandler, request *messages.StorageRequest) {
	log.Println("Attempting to store", request.FileName)

	// Ensure there is enough space available on the disk
	ok, err := hasEnoughSpace(request.FileName, int64(request.Size))
	if err != nil {
		msgHandler.SendResponse(false, err.Error())
		return
	}
	if !ok {
		msgHandler.SendResponse(false, "Not enough disk space on the server")
		return
	}

	file, err := os.OpenFile(request.FileName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0666)
	if err != nil {
		msgHandler.SendResponse(false, err.Error())
		return
	}

	msgHandler.SendResponse(true, "Ready for data")
	md5 := md5.New()
	w := io.MultiWriter(file, md5)

	/* We need to handle the error and exit properly here. Otherwise, the server would then
	   try to read a checksum message but instead read file data, causing unexpected errors.

	   e.g., When trying to store a large file on orion machines, user quota limit will exit
		the file bytes write early. Without error check and handling, the server doesn't know
		about it and will keep receiving the checksum, and msgHandler.Receive() expects the
		leading 8 bytes to be prefix that indicates the length, but it would be actually the
		remaining file data, which could be a very large number, the massive payload allocation
		at payload := make([]byte, payloadSize) will trigger the panic.
	*/
	bytesWritten, err := io.CopyN(w, msgHandler, int64(request.Size)) /* Write and checksum as we go */
	file.Close()

	if err != nil {
		// Don't need to send error message b/c after the connection close, client won't receive it anyway ...
		// msgHandler.SendResponse(false, fmt.Sprintf("Error receiving file data: %v", err))
		log.Printf("Error receiving file data: %v", err)
		msgHandler.Close()
		return
	}
	if bytesWritten != int64(request.Size) {
		// Don't need to send error message b/c after the connection close, client won't receive it anyway ...
		// msgHandler.SendResponse(false, fmt.Sprintf("Incomplete transfer: expected %d bytes, got %d bytes", request.Size, bytesWritten))
		log.Printf("Incomplete transfer: expected %d bytes, stored %d bytes", request.Size, bytesWritten)
		msgHandler.Close()
		return
	}

	serverCheck := md5.Sum(nil)

	clientCheckMsg, _ := msgHandler.Receive()
	clientCheck := clientCheckMsg.GetChecksum().Checksum

	if util.VerifyChecksum(serverCheck, clientCheck) {
		msg := "Successfully stored file."
		log.Println(msg)
		msgHandler.SendResponse(true, msg)
	} else {
		msg := "FAILED to store file. Invalid checksum."
		log.Println(msg)
		msgHandler.SendResponse(false, msg)
	}
}

func handleRetrieval(msgHandler *messages.MessageHandler, request *messages.RetrievalRequest) {
	log.Println("Attempting to retrieve", request.FileName)

	// Get file size and make sure it exists
	info, err := os.Stat(request.FileName)
	if err != nil {
		log.Printf("File not found: %v", err)  
		msgHandler.SendRetrievalResponse(false, "Requested file doesn't exist", 0)
		msgHandler.Close()
		return
	}

	msgHandler.SendRetrievalResponse(true, "Ready to send", uint64(info.Size()))

	// The following block of code read the data from file, write it into w,
	// and w ditributes the data to msgHandler and md5, repectively
	// streaming, send file without loading the entire file into memory
	file, _ := os.Open(request.FileName)
	// md5 is a crypt hash func that converts input data of any size into a fixed-length 128-bit (16-byte) hash value
	md5 := md5.New()
	w := io.MultiWriter(msgHandler, md5)
	// io.CopyN is responsible for the streaming loop; it repeatedly reads from the source and
	// repeatedly calls Write on the destination (which ultimately calls msgHandler.Write).
	bytesRead, err := io.CopyN(w, file, info.Size()) // Checksum and transfer file at same time
	file.Close()

	if err != nil {
		log.Printf("Error sending file data: %v", err)
		msgHandler.Close()
		return
	}
	if bytesRead != int64(info.Size()) {
		log.Printf("Incomplete transfer: expected %d bytes, sent %d bytes", info.Size(), bytesRead)
		msgHandler.Close()
		return
	}

	checksum := md5.Sum(nil)
	msgHandler.SendChecksumVerification(checksum)
}

func handleClient(msgHandler *messages.MessageHandler) {
	defer msgHandler.Close()

	for {
		wrapper, err := msgHandler.Receive()
		if err != nil {
			log.Println(err)
		}

		switch msg := wrapper.Msg.(type) {
		case *messages.Wrapper_StorageReq:
			handleStorage(msgHandler, msg.StorageReq)
			continue
		case *messages.Wrapper_RetrievalReq:
			handleRetrieval(msgHandler, msg.RetrievalReq)
			continue
		case nil:
			log.Println("Received an empty message, terminating client")
			return
		default:
			log.Printf("Unexpected message type: %T", msg)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Not enough arguments. Usage: %s port [download-dir]\n", os.Args[0])
		os.Exit(1)
	}

	port := os.Args[1]
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalln(err.Error())
		os.Exit(1)
	}
	defer listener.Close()

	dir := "."
	if len(os.Args) >= 3 {
		dir = os.Args[2]
	}
	if err := os.Chdir(dir); err != nil {
		log.Fatalln(err)
	}

	fmt.Println("Listening on port:", port)
	fmt.Println("Download directory:", dir)
	for {
		if conn, err := listener.Accept(); err == nil {
			log.Println("Accepted connection", conn.RemoteAddr())
			handler := messages.NewMessageHandler(conn)
			go handleClient(handler)
		}
	}
}
