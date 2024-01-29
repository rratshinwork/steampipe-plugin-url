package url

/*

Copyright (c) 2013, Donovan Jimenez
All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

 * Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.
 * Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

*/

/*
#cgo darwin LDFLAGS: -liconv
#cgo freebsd LDFLAGS: -liconv
#cgo windows LDFLAGS: -liconv
#include <stdlib.h>
#include <iconv.h>

// As of GO 1.6 passing a pointer to Go pointer, will lead to panic
// Therofore we use this wrapper function, to avoid passing **char directly from go
size_t call_iconv(iconv_t ctx, char *in, size_t *size_in, char *out, size_t *size_out){
	return iconv(ctx, &in, size_in, &out, size_out);
}

*/
import "C"
import "io"
import "syscall"
import "unsafe"

type Converter struct {
	context C.iconv_t
	open    bool
}

// Initialize a new Converter. If fromEncoding or toEncoding are not supported by
// iconv then an EINVAL error will be returned. An ENOMEM error maybe returned if
// there is not enough memory to initialize an iconv descriptor
func NewConverter(fromEncoding string, toEncoding string) (converter *Converter, err error) {
	converter = new(Converter)

	// convert to C strings
	toEncodingC := C.CString(toEncoding)
	fromEncodingC := C.CString(fromEncoding)

	// open an iconv descriptor
	converter.context, err = C.iconv_open(toEncodingC, fromEncodingC)

	// free the C Strings
	C.free(unsafe.Pointer(toEncodingC))
	C.free(unsafe.Pointer(fromEncodingC))

	// check err
	if err == nil {
		// no error, mark the context as open
		converter.open = true
	}

	return
}

// destroy is called during garbage collection
func (this *Converter) destroy() {
	this.Close()
}

// Close a Converter's iconv description explicitly
func (this *Converter) Close() (err error) {
	if this.open {
		_, err = C.iconv_close(this.context)
	}

	return
}

// Convert bytes from an input byte slice into a give output byte slice
//
// As many bytes that can converted and fit into the size of output will be
// processed and the number of bytes read for input as well as the number of
// bytes written to output will be returned. If not all converted bytes can fit
// into output and E2BIG error will also be returned. If input contains an invalid
// sequence of bytes for the Converter's fromEncoding an EILSEQ error will be returned
//
// For shift based output encodings, any end shift byte sequences can be generated by
// passing a 0 length byte slice as input. Also passing a 0 length byte slice for output
// will simply reset the iconv descriptor shift state without writing any bytes.
func (this *Converter) Convert(input []byte, output []byte) (bytesRead int, bytesWritten int, err error) {
	// make sure we are still open
	if this.open {
		inputLeft := C.size_t(len(input))
		outputLeft := C.size_t(len(output))

		if inputLeft > 0 && outputLeft > 0 {
			// we have to give iconv a pointer to a pointer of the underlying
			// storage of each byte slice - so far this is the simplest
			// way i've found to do that in Go, but it seems ugly
			inputPointer := (*C.char)(unsafe.Pointer(&input[0]))
			outputPointer := (*C.char)(unsafe.Pointer(&output[0]))

			_, err = C.call_iconv(this.context, inputPointer, &inputLeft, outputPointer, &outputLeft)

			// update byte counters
			bytesRead = len(input) - int(inputLeft)
			bytesWritten = len(output) - int(outputLeft)
		} else if inputLeft == 0 && outputLeft > 0 {
			// inputPointer will be nil, outputPointer is generated as above
			outputPointer := (*C.char)(unsafe.Pointer(&output[0]))

			_, err = C.call_iconv(this.context, nil, &inputLeft, outputPointer, &outputLeft)

			// update write byte counter
			bytesWritten = len(output) - int(outputLeft)
		} else {
			// both input and output are zero length, do a shift state reset
			_, err = C.call_iconv(this.context, nil, &inputLeft, nil, &outputLeft)
		}
	} else {
		err = syscall.EBADF
	}

	return bytesRead, bytesWritten, err
}

// Convert an input string
//
// EILSEQ error may be returned if input contains invalid bytes for the
// Converter's fromEncoding.
func (this *Converter) ConvertString(input string) (output string, err error) {
	// make sure we are still open
	if this.open {
		// construct the buffers
		inputBuffer := []byte(input)
		outputBuffer := make([]byte, len(inputBuffer)*2) // we use a larger buffer to help avoid resizing later

		// call Convert until all input bytes are read or an error occurs
		var bytesRead, totalBytesRead, bytesWritten, totalBytesWritten int

		for totalBytesRead < len(inputBuffer) && err == nil {
			// use the totals to create buffer slices
			bytesRead, bytesWritten, err = this.Convert(inputBuffer[totalBytesRead:], outputBuffer[totalBytesWritten:])

			totalBytesRead += bytesRead
			totalBytesWritten += bytesWritten

			// check for the E2BIG error specifically, we can add to the output
			// buffer to correct for it and then continue
			if err == syscall.E2BIG {
				// increase the size of the output buffer by another input length
				// first, create a new buffer
				tempBuffer := make([]byte, len(outputBuffer)+len(inputBuffer))

				// copy the existing data
				copy(tempBuffer, outputBuffer)

				// switch the buffers
				outputBuffer = tempBuffer

				// forget the error
				err = nil
			}
		}

		if err == nil {
			// perform a final shift state reset
			_, bytesWritten, err = this.Convert([]byte{}, outputBuffer[totalBytesWritten:])

			// update total count
			totalBytesWritten += bytesWritten
		}

		// construct the final output string
		output = string(outputBuffer[:totalBytesWritten])
	} else {
		err = syscall.EBADF
	}

	return output, err
}




// All in one Convert method, rather than requiring the construction of an iconv.Converter
func Convert(input []byte, output []byte, fromEncoding string, toEncoding string) (bytesRead int, bytesWritten int, err error) {
	// create a temporary converter
	converter, err := NewConverter(fromEncoding, toEncoding)

	if err == nil {
		// call converter's Convert
		bytesRead, bytesWritten, err = converter.Convert(input, output)

		if err == nil {
			var shiftBytesWritten int

			// call Convert with a nil input to generate any end shift sequences
			_, shiftBytesWritten, err = converter.Convert(nil, output[bytesWritten:])

			// add shift bytes to total bytes
			bytesWritten += shiftBytesWritten
		}

		// close the converter
		converter.Close()
	}

	return
}

// All in one ConvertString method, rather than requiring the construction of an iconv.Converter
func ConvertString(input string, fromEncoding string, toEncoding string) (output string, err error) {
	// create a temporary converter
	converter, err := NewConverter(fromEncoding, toEncoding)

	if err == nil {
		// convert the string
		output, err = converter.ConvertString(input)

		// close the converter
		converter.Close()
	}

	return
}


type Reader struct {
	source            io.Reader
	converter         *Converter
	buffer            []byte
	readPos, writePos int
	err               error
}

func NewReader(source io.Reader, fromEncoding string, toEncoding string) (*Reader, error) {
	// create a converter
	converter, err := NewConverter(fromEncoding, toEncoding)

	if err == nil {
		return NewReaderFromConverter(source, converter), err
	}

	// return the error
	return nil, err
}

func NewReaderFromConverter(source io.Reader, converter *Converter) (reader *Reader) {
	reader = new(Reader)

	// copy elements
	reader.source = source
	reader.converter = converter

	// create 8K buffers
	reader.buffer = make([]byte, 8*1024)

	return reader
}

func (this *Reader) fillBuffer() {
	// slide existing data to beginning
	if this.readPos > 0 {
		// copy current bytes - is this guaranteed safe?
		copy(this.buffer, this.buffer[this.readPos:this.writePos])

		// adjust positions
		this.writePos -= this.readPos
		this.readPos = 0
	}

	// read new data into buffer at write position
	bytesRead, err := this.source.Read(this.buffer[this.writePos:])

	// adjust write position
	this.writePos += bytesRead

	// track any reader error / EOF
	if err != nil {
		this.err = err
	}
}

// implement the io.Reader interface
func (this *Reader) Read(p []byte) (n int, err error) {
	// checks for when we have no data
	for this.writePos == 0 || this.readPos == this.writePos {
		// if we have an error / EOF, just return it
		if this.err != nil {
			return n, this.err
		}

		// else, fill our buffer
		this.fillBuffer()
	}

	// TODO: checks for when we have less data than len(p)

	// we should have an appropriate amount of data, convert it into the given buffer
	bytesRead, bytesWritten, err := this.converter.Convert(this.buffer[this.readPos:this.writePos], p)

	// adjust byte counters
	this.readPos += bytesRead
	n += bytesWritten

	// if we experienced an iconv error, check it
	if err != nil {
		// E2BIG errors can be ignored (we'll get them often) as long
		// as at least 1 byte was written. If we experienced an E2BIG
		// and no bytes were written then the buffer is too small for
		// even the next character
		if err != syscall.E2BIG || bytesWritten == 0 {
			// track anything else
			this.err = err
		}
	}

	// return our results
	return n, this.err
}


type Writer struct {
	destination       io.Writer
	converter         *Converter
	buffer            []byte
	readPos, writePos int
	err               error
}

func NewWriter(destination io.Writer, fromEncoding string, toEncoding string) (*Writer, error) {
	// create a converter
	converter, err := NewConverter(fromEncoding, toEncoding)

	if err == nil {
		return NewWriterFromConverter(destination, converter), err
	}

	// return the error
	return nil, err
}

func NewWriterFromConverter(destination io.Writer, converter *Converter) (writer *Writer) {
	writer = new(Writer)

	// copy elements
	writer.destination = destination
	writer.converter = converter

	// create 8K buffers
	writer.buffer = make([]byte, 8*1024)

	return writer
}

func (this *Writer) emptyBuffer() {
	// write new data out of buffer
	bytesWritten, err := this.destination.Write(this.buffer[this.readPos:this.writePos])

	// update read position
	this.readPos += bytesWritten

	// slide existing data to beginning
	if this.readPos > 0 {
		// copy current bytes - is this guaranteed safe?
		copy(this.buffer, this.buffer[this.readPos:this.writePos])

		// adjust positions
		this.writePos -= this.readPos
		this.readPos = 0
	}

	// track any reader error / EOF
	if err != nil {
		this.err = err
	}
}

// implement the io.Writer interface
func (this *Writer) Write(p []byte) (n int, err error) {
	// write data into our internal buffer
	bytesRead, bytesWritten, err := this.converter.Convert(p, this.buffer[this.writePos:])

	// update bytes written for return
	n += bytesRead
	this.writePos += bytesWritten

	// checks for when we have a full buffer
	for this.writePos > 0 {
		// if we have an error, just return it
		if this.err != nil {
			return
		}

		// else empty the buffer
		this.emptyBuffer()
	}

	return n, err
}