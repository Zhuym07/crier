package spool

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var dll = syscall.NewLazyDLL("winspool.drv")
var enum = dll.NewProc("EnumPrintersW")
var open = dll.NewProc("OpenPrinterW")
var closePrinter = dll.NewProc("ClosePrinter")
var start = dll.NewProc("StartDocPrinterW")
var end = dll.NewProc("EndDocPrinter")
var abort = dll.NewProc("AbortPrinter")
var write = dll.NewProc("WritePrinter")

type printerInfo struct {
	Flags                      uint32
	Description, Name, Comment *uint16
}
type documentInfo struct{ Name, Output, DataType *uint16 }

func List() ([]string, error) {
	var needed, count uint32
	const flags = 2 | 4 // local printers and connected printers
	result, _, err := enum.Call(flags, 0, 1, 0, 0, uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if needed == 0 {
		if result == 0 {
			return nil, err
		}
		return []string{}, nil
	}
	buffer := make([]byte, needed)
	result, _, err = enum.Call(flags, 0, 1, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)))
	if result == 0 {
		return nil, err
	}
	names := make([]string, 0, count)
	for _, printer := range unsafe.Slice((*printerInfo)(unsafe.Pointer(&buffer[0])), count) {
		if printer.Name == nil {
			continue
		}
		var n int
		for *(*uint16)(unsafe.Add(unsafe.Pointer(printer.Name), n*2)) != 0 {
			n++
		}
		names = append(names, syscall.UTF16ToString(unsafe.Slice(printer.Name, n)))
	}
	runtime.KeepAlive(buffer)
	return names, nil
}

func Print(name string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty print job")
	}
	target, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	var handle uintptr
	result, _, err := open.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(&handle)), 0)
	if result == 0 {
		return fmt.Errorf("open printer: %w", err)
	}
	defer closePrinter.Call(handle)
	title, _ := syscall.UTF16PtrFromString("Crier")
	raw, _ := syscall.UTF16PtrFromString("RAW")
	doc := documentInfo{Name: title, DataType: raw}
	result, _, err = start.Call(handle, 1, uintptr(unsafe.Pointer(&doc)))
	runtime.KeepAlive(doc)
	if result == 0 {
		return fmt.Errorf("start print job: %w", err)
	}
	var written uint32
	result, _, err = write.Call(handle, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	if result == 0 || int(written) != len(data) {
		abort.Call(handle)
		if result == 0 {
			return fmt.Errorf("write print job: %w", err)
		}
		return fmt.Errorf("short print write: %d of %d bytes", written, len(data))
	}
	result, _, err = end.Call(handle)
	if result == 0 {
		abort.Call(handle)
		return fmt.Errorf("submit print job: %w", err)
	}
	return nil
}
