package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/rudderlabs/rudder-server/utils/logger"
	"github.com/rudderlabs/rudder-server/utils/misc"
)

type transformerResponseT struct {
	output []byte
	err    string
}

type transformerBatchResponseT struct {
	batchError error
	responses  []transformerResponseT
}

func (webhook *webhookHandleT) transform(events [][]byte, sourceType string) transformerBatchResponseT {
	webhook.sentStat.Count(len(events))
	webhook.transformTimerStat.Start()

	payload := misc.MakeJSONArray(events)
	url := fmt.Sprintf(`%s/%s`, sourceTransformerURL, strings.ToLower(sourceType))
	resp, err := webhook.netClient.Post(url, "application/json; charset=utf-8", bytes.NewBuffer(payload))

	webhook.transformTimerStat.End()
	if err != nil {
		logger.Error(err)
		webhook.failedStat.Count(len(events))
		return transformerBatchResponseT{batchError: errors.New("Internal server error in source transformer")}
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		webhook.failedStat.Count(len(events))
		return transformerBatchResponseT{batchError: err}
	}

	var responses []interface{}
	err = json.Unmarshal(respBody, &responses)

	batchResponse := transformerBatchResponseT{responses: make([]transformerResponseT, len(events))}

	if len(responses) != len(events) {
		panic("Source rudder-transformer response size does not equal sent events size")
	}

	for idx, response := range responses {
		respElemMap, castOk := response.(map[string]interface{})
		if castOk {
			outputInterface, ok := respElemMap["output"]
			if !ok {
				batchResponse.responses[idx] = transformerResponseT{err: getStatus(SourceTrasnformerResponseReadFailed)}
				webhook.failedStat.Count(1)
				continue
			}

			output, ok := outputInterface.(map[string]interface{})
			if !ok {
				batchResponse.responses[idx] = transformerResponseT{err: getStatus(SourceTrasnformerResponseReadFailed)}
				webhook.failedStat.Count(1)
				continue
			}

			if statusCode, found := output["statusCode"]; found && fmt.Sprintf("%v", statusCode) == "400" {
				var errorMessage interface{}
				if errorMessage, ok = output["error"]; !ok {
					errorMessage = getStatus(SourceTrasnformerResponseReadFailed)
				}
				batchResponse.responses[idx] = transformerResponseT{err: fmt.Sprintf("%v", errorMessage)}
				webhook.failedStat.Count(1)
				continue
			}
			webhook.receivedStat.Count(1)
			marshalledOutput, _ := json.Marshal(output)
			batchResponse.responses[idx] = transformerResponseT{output: marshalledOutput}
		} else {
			batchResponse.responses[idx] = transformerResponseT{err: getStatus(SourceTrasnformerResponseReadFailed)}
			webhook.failedStat.Count(1)
		}
	}
	return batchResponse
}