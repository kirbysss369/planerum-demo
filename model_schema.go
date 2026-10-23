package hours

import "sort"

// gatewayResponseFormat requests the exact JSON shapes consumed by the
// generator. The GigaSchool gateway accepts the OpenAI-compatible wrapper.
func gatewayResponseFormat(output any) (map[string]any, bool) {
	stringField := func() map[string]any { return map[string]any{"type": "string"} }
	stringArray := func() map[string]any {
		return map[string]any{"type": "array", "items": stringField()}
	}
	object := func(properties map[string]any) map[string]any {
		required := make([]string, 0, len(properties))
		for name := range properties {
			required = append(required, name)
		}
		sort.Strings(required)
		return map[string]any{
			"type": "object", "properties": properties,
			"required": required, "additionalProperties": false,
		}
	}
	array := func(items any) map[string]any {
		return map[string]any{"type": "array", "items": items}
	}
	source := object(map[string]any{
		"id": stringField(), "title": stringField(),
		"location": stringField(),
		"status":   map[string]any{"type": "string", "enum": []string{"unverified"}},
	})
	sourceFields := source["properties"].(map[string]any)
	for field, limit := range map[string]int{"id": 64, "title": 150, "location": 250} {
		sourceFields[field].(map[string]any)["maxLength"] = limit
	}
	boundedSources := array(source)
	boundedSources["maxItems"] = 1
	var name string
	var schema map[string]any
	switch output.(type) {
	case *ParsedSpec:
		bounded := func(limit int) map[string]any {
			return map[string]any{"type": "string", "maxLength": limit}
		}
		name = "parsed_program_spec"
		schema = object(map[string]any{
			"spec": object(map[string]any{
				"title":                  bounded(intakeTitleLimit),
				"kind":                   map[string]any{"type": "string", "enum": []string{"", string(Qualification), string(General)}},
				"audience":               bounded(intakeTextLimit),
				"entry_level":            map[string]any{"type": "string", "maxLength": intakeTextLimit, "description": "Имеющиеся знания и опыт, в том числе их отсутствие: владеют Excel, опыта SQL нет. Заполняется отдельно от профессии/роли аудитории."},
				"delivery":               bounded(intakeTextLimit),
				"requested":              map[string]any{"type": "number"},
				"independent_internship": map[string]any{"type": "boolean"},
				"attestation":            bounded(intakeTextLimit),
				"requirements":           bounded(intakeRequirementsLimit),
			}),
		})
	case *GeneratedOutline:
		activity := object(map[string]any{
			"id": stringField(), "name": stringField(),
			"kind":  map[string]any{"type": "string", "enum": []string{"lecture", "practice", "laboratory", "self_study"}},
			"hours": map[string]any{"type": "integer", "minimum": 1},
		})
		topic := object(map[string]any{
			"id": stringField(), "title": stringField(), "activities": array(activity),
		})
		topic["properties"].(map[string]any)["activities"].(map[string]any)["minItems"] = 1
		module := object(map[string]any{
			"id": stringField(), "title": stringField(), "topics": array(topic),
		})
		module["properties"].(map[string]any)["topics"].(map[string]any)["minItems"] = 1
		outcome := object(map[string]any{
			"id": stringField(), "text": stringField(),
			"knowledge": stringArray(), "skills": stringArray(),
			"experience": stringArray(), "topic_ids": stringArray(),
		})
		outcomeFields := outcome["properties"].(map[string]any)
		for _, name := range []string{"knowledge", "skills", "experience", "topic_ids"} {
			outcomeFields[name].(map[string]any)["minItems"] = 1
		}
		outcomes := array(outcome)
		outcomes["minItems"] = 3
		name = "program_outline"
		schema = object(map[string]any{
			"goal": stringField(), "outcomes": outcomes,
			"modules":                 array(module),
			"final_attestation_hours": map[string]any{"type": "integer", "minimum": 0},
			"sources":                 boundedSources,
		})
	case *GeneratedAssessmentBatch:
		question, answer, rubric := stringField(), stringField(), stringField()
		question["minLength"], question["maxLength"] = 1, 350
		answer["maxLength"], rubric["maxLength"] = 500, 500
		outcomeIDs := stringArray()
		outcomeIDs["minItems"], outcomeIDs["maxItems"] = 1, 1
		outcomeIDs["items"].(map[string]any)["maxLength"] = 64
		assessment := object(map[string]any{
			"kind":     map[string]any{"type": "string", "enum": []string{"test", "practice"}},
			"question": question, "answer": answer,
			"rubric": rubric, "outcome_ids": outcomeIDs,
		})
		name = "assessment_batch"
		assessments := array(assessment)
		assessments["minItems"] = 2
		assessments["maxItems"] = 2
		schema = object(map[string]any{
			"assessments": assessments, "sources": boundedSources,
		})
	case *GeneratedLectureBatch:
		plan := stringField()
		plan["minLength"] = 1
		plan["maxLength"] = 900
		slides := stringArray()
		slides["minItems"] = 3
		slides["maxItems"] = 5
		slide := stringField()
		slide["minLength"] = 1
		slide["maxLength"] = 350
		slides["items"] = slide
		lecture := object(map[string]any{
			"topic_id": stringField(), "plan": plan, "slides": slides,
		})
		lecture["properties"].(map[string]any)["topic_id"].(map[string]any)["maxLength"] = 64
		lectures := array(lecture)
		lectures["minItems"] = 1
		lectures["maxItems"] = 1
		name = "lecture_batch"
		schema = object(map[string]any{
			"lectures": lectures, "sources": boundedSources,
		})
	default:
		return nil, false
	}
	return map[string]any{
		"type":        "json_schema",
		"json_schema": map[string]any{"name": name, "schema": schema, "strict": true},
	}, true
}

// Source locations must come from the supplied references, not from topic IDs
// or bibliographic details invented by the model.
func gatewayResponseFormatForInput(input, output any) (map[string]any, bool) {
	format, ok := gatewayResponseFormat(output)
	if !ok {
		return format, ok
	}
	var spec ProgramSpec
	switch in := input.(type) {
	case ProgramSpec:
		spec = in
	case AssessmentInput:
		spec = in.Spec
	case LectureInput:
		spec = in.Spec
	default:
		return format, ok
	}
	root := format["json_schema"].(map[string]any)["schema"].(map[string]any)
	if _, outline := output.(*GeneratedOutline); outline {
		final, budgets := plannedOutlineHours(spec.Requested)
		if len(budgets) > 0 {
			properties := root["properties"].(map[string]any)
			modules := properties["modules"].(map[string]any)
			modules["minItems"], modules["maxItems"] = len(budgets)/2, len(budgets)/2
			topics := modules["items"].(map[string]any)["properties"].(map[string]any)["topics"].(map[string]any)
			topics["minItems"], topics["maxItems"] = 2, 2
			properties["final_attestation_hours"].(map[string]any)["enum"] = []Hours{final}
		}
	}
	sources, ok := root["properties"].(map[string]any)["sources"].(map[string]any)
	if !ok {
		return format, true
	}
	locations := suppliedSourceLocations(spec)
	if len(locations) == 0 {
		sources["maxItems"] = 0
	} else {
		fields := sources["items"].(map[string]any)["properties"].(map[string]any)
		fields["location"].(map[string]any)["enum"] = locations
	}
	return format, true
}
