/*
Copyright 2023 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in
compliance with the License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is
distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing permissions and limitations under the
License.
*/

package logging

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/ghttp"
)

var _ = Describe("Transport wrapper", func() {
	It("Can't be created without a logger", func() {
		wrapper, err := NewTransportWrapper().Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("logger"))
		Expect(msg).To(ContainSubstring("mandatory"))
		Expect(wrapper).To(BeNil())
	})

	It("Can't be created with negative header v-level", func() {
		// Create the logger:
		logger, err := NewLogger().
			SetWriter(io.Discard).
			SetV(math.MaxInt).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Try to create the wrapper:
		wrapper, err := NewTransportWrapper().
			SetLogger(logger).
			SetHeaderV(-1).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("header"))
		Expect(msg).To(ContainSubstring("-1"))
		Expect(msg).To(ContainSubstring("must be greater than or equal to 0"))
		Expect(wrapper).To(BeNil())
	})

	It("Can't be created with negative body v-level", func() {
		// Create the logger:
		logger, err := NewLogger().
			SetWriter(io.Discard).
			SetV(math.MaxInt).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Try to create the wrapper:
		wrapper, err := NewTransportWrapper().
			SetLogger(logger).
			SetBodyV(-1).
			Build()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring("body"))
		Expect(msg).To(ContainSubstring("-1"))
		Expect(msg).To(ContainSubstring("must be greater than or equal to 0"))
		Expect(wrapper).To(BeNil())
	})

	Context("With server", func() {
		var (
			server *Server
			buffer *bytes.Buffer
			client *http.Client
		)

		BeforeEach(func() {
			// Create the server:
			server = NewServer()

			// Create a logger that writes to the Ginkgo writer and also a buffer in
			// memory, so that we can analyze the result:
			buffer = &bytes.Buffer{}
			logger, err := NewLogger().
				SetWriter(io.MultiWriter(buffer, GinkgoWriter)).
				SetV(math.MaxInt).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create the client:
			wrapper, err := NewTransportWrapper().
				SetLogger(logger).
				SetHeaderV(15).
				SetBodyV(16).
				Build()
			Expect(err).ToNot(HaveOccurred())
			transport := wrapper.Wrap(http.DefaultTransport)

			// Create the client:
			client = &http.Client{
				Transport: transport,
			}
		})

		AfterEach(func() {
			// Stop the server:
			server.Close()
		})

		It("Writes the details of the request line", func() {
			// Prepare the server:
			server.AppendHandlers(RespondWith(http.StatusOK, nil))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Get(url)
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the request line details:
			messages := Parse(buffer)
			details := Find(messages, "Sending request header")
			Expect(details).To(HaveLen(1))
			detail := details[0]
			Expect(detail).To(HaveKeyWithValue("method", http.MethodGet))
			Expect(detail).To(HaveKeyWithValue("url", url))
		})

		It("Writes the details of the response line", func() {
			// Prepare the server:
			server.AppendHandlers(RespondWith(http.StatusOK, nil))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Get(url)
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the response line details:
			messages := Parse(buffer)
			details := Find(messages, "Received response header")
			Expect(details).ToNot(BeEmpty())
			detail := details[0]
			Expect(detail).To(HaveKeyWithValue("protocol", "HTTP/1.1"))
			Expect(detail).To(HaveKeyWithValue("status", "200 OK"))
			Expect(detail).To(HaveKeyWithValue("code", BeNumerically("==", http.StatusOK)))
		})

		It("Writes the size of the request body chunks", func() {
			// Prepare the server:
			server.AppendHandlers(RespondWith(http.StatusOK, nil))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			body := make([]byte, 42)
			response, err := client.Post(url, "application/octet-stream", bytes.NewBuffer(body))
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the number of bytes. Note that there may be multiple lines like
			// this if the response body was split into multiple network packages, so we
			// will sum the values of the `n` fields and count the total.
			messages := Parse(buffer)
			details := Find(messages, "Sending request body")
			Expect(details).ToNot(BeEmpty())
			total := 0
			for _, detail := range details {
				Expect(detail).To(HaveKeyWithValue("n", BeNumerically(">=", 0)))
				total += int(detail["n"].(float64))
			}
			Expect(total).To(Equal(len(body)))
		})

		It("Writes the size of the response body chunks", func() {
			// Prepare the server:
			body := make([]byte, 42)
			server.AppendHandlers(RespondWith(http.StatusOK, body))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Post(url, "application/octet-stream", bytes.NewBuffer(body))
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the number of bytes. Note that there may be multiple lines like
			// this if the response body was split into multiple network packages, so we
			// will sum the values of the `n` fields and count the total.
			messages := Parse(buffer)
			details := Find(messages, "Received response body")
			Expect(details).ToNot(BeEmpty())
			total := 0
			for _, detail := range details {
				Expect(detail).To(HaveKeyWithValue("n", BeNumerically(">=", 0)))
				total += int(detail["n"].(float64))
			}
			Expect(total).To(Equal(len(body)))
		})

		It("Writes request and response identifier", func() {
			// Prepare the server:
			body := make([]byte, 42)
			server.AppendHandlers(RespondWith(http.StatusOK, body))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Post(url, "application/octet-stream", bytes.NewBuffer(body))
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// All the messages should contain the same identifier:
			messages := Parse(buffer)
			Expect(messages).ToNot(BeEmpty())
			first := messages[0]
			Expect(first).To(HaveKey("id"))
			id := first["id"]
			Expect(id).ToNot(BeEmpty())
			for i := 1; i < len(messages); i++ {
				Expect(messages[i]).To(HaveKeyWithValue("id", id))
			}
		})

		It("Honors the header v-level", func() {
			// Prepare the server:
			server.AppendHandlers(RespondWith(http.StatusOK, nil))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Get(url)
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the v-level:
			messages := Parse(buffer)
			Expect(messages).ToNot(BeEmpty())
			requests := Find(messages, "Sending request header")
			Expect(requests).ToNot(BeEmpty())
			for _, message := range requests {
				Expect(message).To(HaveKeyWithValue("v", BeNumerically("==", 15)))
			}
			responses := Find(messages, "Received response header")
			for _, message := range responses {
				Expect(message).To(HaveKeyWithValue("v", BeNumerically("==", 15)))
			}
		})

		It("Honors the body v-level", func() {
			// Prepare the server:
			body := make([]byte, 42)
			server.AppendHandlers(RespondWith(http.StatusOK, body))

			// Send the request:
			url := fmt.Sprintf("%s/my-path", server.URL())
			response, err := client.Post(url, "application/octet-stream", bytes.NewBuffer(body))
			Expect(err).ToNot(HaveOccurred())
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			Expect(err).ToNot(HaveOccurred())

			// Verify the v-level:
			messages := Parse(buffer)
			Expect(messages).ToNot(BeEmpty())
			requests := Find(messages, "Sending request body")
			for _, message := range requests {
				Expect(message).To(HaveKeyWithValue("v", BeNumerically("==", 16)))
			}
			responses := Find(messages, "Received response body")
			for _, message := range responses {
				Expect(message).To(HaveKeyWithValue("v", BeNumerically("==", 16)))
			}
		})
	})
})
