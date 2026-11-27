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
	_, err := Submit(name, "Crier", data)
	return err
}

func openHandle(name string) (uintptr, error) {
	target, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var handle uintptr
	result, _, callErr := open.Call(uintptr(unsafe.Pointer(target)), uintptr(unsafe.Pointer(&handle)), 0)
	if result == 0 {
		return 0, fmt.Errorf("open printer: %w", callErr)
	}
	return handle, nil
}

func Submit(name, document string, data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, fmt.Errorf("empty print job")
	}
	handle, err := openHandle(name)
	if err != nil {
		return 0, err
	}
	defer closePrinter.Call(handle)
	title, err := syscall.UTF16PtrFromString(document)
	if err != nil {
		return 0, err
	}
	raw, _ := syscall.UTF16PtrFromString("RAW")
	doc := documentInfo{Name: title, DataType: raw}
	id, _, callErr := start.Call(handle, 1, uintptr(unsafe.Pointer(&doc)))
	runtime.KeepAlive(doc)
	if id == 0 {
		return 0, fmt.Errorf("start print job: %w", callErr)
	}
	var written uint32
	result, _, callErr := write.Call(handle, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	if result == 0 || int(written) != len(data) {
		abort.Call(handle)
		if result == 0 {
			return 0, fmt.Errorf("write print job: %w", callErr)
		}
		return 0, fmt.Errorf("short print write: %d of %d bytes", written, len(data))
	}
	result, _, callErr = end.Call(handle)
	if result == 0 {
		abort.Call(handle)
		return 0, fmt.Errorf("submit print job: %w", callErr)
	}
	return uint32(id), nil
}

var getJob = dll.NewProc("GetJobW")

type jobInfo1 struct {
	ID                                                     uint32
	Printer, Machine, User, Document, DataType, StatusText *uint16
	Status                                                 uint32
}

// ReportedPrinted checks the driver's explicit PRINTED flag. A missing job
// or the COMPLETE flag alone does not establish that the printer printed it.
func ReportedPrinted(printer string, id uint32, document string) (printed, tracked bool) {
	handle, err := openHandle(printer)
	if err != nil {
		return false, true
	}
	defer closePrinter.Call(handle)
	var needed uint32
	_, _, callErr := getJob.Call(handle, uintptr(id), 1, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 {
		return false, callErr != syscall.Errno(87)
	}
	if uintptr(needed) < unsafe.Sizeof(jobInfo1{}) || needed > 1<<20 {
		return false, true
	}
	buf := make([]byte, needed)
	result, _, _ := getJob.Call(handle, uintptr(id), 1, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&needed)))
	if result == 0 {
		return false, true
	}
	info := (*jobInfo1)(unsafe.Pointer(&buf[0]))
	if info.Document == nil {
		return false, false
	}
	expected, err := syscall.UTF16FromString(document)
	if err != nil {
		return false, false
	}
	base, ptr := uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(info.Document))
	if ptr < base || ptr-base+uintptr(len(expected))*2 > uintptr(len(buf)) {
		return false, false
	}
	for i, char := range expected {
		if unsafe.Slice(info.Document, len(expected))[i] != char {
			return false, false
		}
	}
	return info.Status&0x80 != 0, true
}
