/*
Copyright 2022 The Tekton Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// mergeData is used to store the intermediate data needed to merge an object
// with a template. It's provided to avoid repeatedly re-serializing the template.
// +k8s:openapi-gen=false
type mergeData struct {
	emptyJSON    []byte
	templateJSON []byte
	patchSchema  strategicpatch.PatchMetaFromStruct
}

// MergeStepsWithStepTemplate takes a possibly nil container template and a
// list of steps, merging each of the steps with the container template, if
// it's not nil, and returning the resulting list.
func MergeStepsWithStepTemplate(template *StepTemplate, steps []Step) ([]Step, error) {
	if template == nil {
		return steps, nil
	}

	md, err := getMergeData(template.ToK8sContainer(), &corev1.Container{})
	if err != nil {
		return nil, err
	}

	for i, s := range steps {
		merged := corev1.Container{}
		err := mergeObjWithTemplateBytes(md, s.ToK8sContainer(), &merged)
		if err != nil {
			return nil, err
		}

		// If the container's args is nil, reset it to empty instead
		if merged.Args == nil && s.Args != nil {
			merged.Args = []string{}
		}

		// Pass through original step Script, for later conversion.
		newStep := Step{Script: s.Script, OnError: s.OnError, Timeout: s.Timeout}
		newStep.SetContainerFields(merged)
		steps[i] = newStep
	}
	return steps, nil
}

// getMergeData serializes the template and empty object to get the intermediate results necessary for
// merging an object of the same type with this template.
// This function is provided to avoid repeatedly serializing an identical template.
func getMergeData(template, empty interface{}) (*mergeData, error) {
	// We need JSON bytes to generate a patch to merge the object
	// onto the template, so marshal the template.
	templateJSON, err := json.Marshal(template)
	if err != nil {
		return nil, err
	}
	// We need to do a three-way merge to actually merge the template and
	// object, so we need an empty object as the "original"
	emptyJSON, err := json.Marshal(empty)
	if err != nil {
		return nil, err
	}
	// Get the patch meta, which is needed for generating and applying the merge patch.
	patchSchema, err := strategicpatch.NewPatchMetaFromStruct(template)
	if err != nil {
		return nil, err
	}
	return &mergeData{templateJSON: templateJSON, emptyJSON: emptyJSON, patchSchema: patchSchema}, nil
}

// mergeObjWithTemplateBytes merges obj with md's template JSON and updates out to reflect the merged result.
// out is a pointer to the zero value of obj's type.
// This function is provided to avoid repeatedly serializing an identical template.
func mergeObjWithTemplateBytes(md *mergeData, obj, out interface{}) error {
	// Marshal the object to JSON
	objAsJSON, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	// Create a merge patch, with the empty JSON as the original, the object JSON as the modified, and the template
	// JSON as the current - this lets us do a deep merge of the template and object, with awareness of
	// the "patchMerge" tags.
	patch, err := strategicpatch.CreateThreeWayMergePatch(md.emptyJSON, objAsJSON, md.templateJSON, md.patchSchema, true)
	if err != nil {
		return err
	}

	// Actually apply the merge patch to the template JSON.
	mergedAsJSON, err := strategicpatch.StrategicMergePatchUsingLookupPatchMeta(md.templateJSON, patch, md.patchSchema)
	if err != nil {
		return err
	}
	// Unmarshal the merged JSON to a pointer, and return it.
	return json.Unmarshal(mergedAsJSON, out)
}
