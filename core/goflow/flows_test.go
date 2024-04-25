package goflow_test

import (
	"testing"

	"github.com/Masterminds/semver"
	"github.com/nyaruka/gocommon/i18n"
	"github.com/nyaruka/gocommon/uuids"
	"github.com/nyaruka/goflow/assets"
	"github.com/nyaruka/goflow/flows"
	"github.com/nyaruka/goflow/test"
	"github.com/nyaruka/mailroom/core/goflow"
	"github.com/nyaruka/mailroom/testsuite"
	"github.com/stretchr/testify/assert"
)

func TestSpecVersion(t *testing.T) {
	assert.Equal(t, semver.MustParse("13.5.0"), goflow.SpecVersion())
}

func TestReadFlow(t *testing.T) {
	_, rt := testsuite.Runtime()

	// try to read empty definition
	flow, err := goflow.ReadFlow(rt.Config, []byte(`{}`))
	assert.Nil(t, flow)
	assert.EqualError(t, err, "unable to read flow header: field 'uuid' is required, field 'spec_version' is required")

	// read legacy definition
	flow, err = goflow.ReadFlow(rt.Config, []byte(`{"flow_type": "M", "base_language": "eng", "action_sets": [], "metadata": {"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", "name": "Legacy"}}`))
	assert.Nil(t, err)
	assert.Equal(t, assets.FlowUUID("502c3ee4-3249-4dee-8e71-c62070667d52"), flow.UUID())
	assert.Equal(t, "Legacy", flow.Name())
	assert.Equal(t, i18n.Language("eng"), flow.Language())
	assert.Equal(t, flows.FlowTypeMessaging, flow.Type())

	// read new definition
	flow, err = goflow.ReadFlow(rt.Config, []byte(`{"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", "name": "New", "spec_version": "13.0.0", "type": "messaging", "language": "eng", "nodes": []}`))
	assert.Nil(t, err)
	assert.Equal(t, assets.FlowUUID("502c3ee4-3249-4dee-8e71-c62070667d52"), flow.UUID())
	assert.Equal(t, "New", flow.Name())
	assert.Equal(t, i18n.Language("eng"), flow.Language())
}

func TestCloneDefinition(t *testing.T) {
	uuids.SetGenerator(uuids.NewSeededGenerator(12345))
	defer uuids.SetGenerator(uuids.DefaultGenerator)

	cloned, err := goflow.CloneDefinition([]byte(`{"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", "name": "New", "spec_version": "13.0.0", "type": "messaging", "language": "eng", "nodes": []}`), nil)
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{"uuid": "1ae96956-4b34-433e-8d1a-f05fe6923d6d", "name": "New", "spec_version": "13.0.0", "type": "messaging", "language": "eng", "nodes": []}`), cloned)
}

func TestMigrateDefinition(t *testing.T) {
	_, rt := testsuite.Runtime()

	uuids.SetGenerator(uuids.NewSeededGenerator(12345))
	defer uuids.SetGenerator(uuids.DefaultGenerator)

	original := []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.0.0", 
		"type": "messaging", 
		"language": 
		"base", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook",
						"templating": {
							"template": {
								"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
								"name": "welcome"
							},
							"variables": ["@contact.name"]
						}
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`)

	// 13.0 > 13.1
	migrated, err := goflow.MigrateDefinition(rt.Config, original, semver.MustParse("13.1.0"))
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.1.0", 
		"type": "messaging", 
		"language": "base", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook",
						"templating": {
							"uuid": "1ae96956-4b34-433e-8d1a-f05fe6923d6d",
							"template": {
								"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
								"name": "welcome"
							},
							"variables": ["@contact.name"]
						}
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`), migrated)

	// 13.1 > 13.2
	migrated, err = goflow.MigrateDefinition(rt.Config, migrated, semver.MustParse("13.2.0"))
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.2.0", 
		"type": "messaging", 
		"language": "und", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook",
						"templating": {
							"uuid": "1ae96956-4b34-433e-8d1a-f05fe6923d6d",
							"template": {
								"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
								"name": "welcome"
							},
							"variables": ["@contact.name"]
						}
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`), migrated)

	// 13.2 > 13.3
	migrated, err = goflow.MigrateDefinition(rt.Config, migrated, semver.MustParse("13.3.0"))
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.3.0", 
		"type": "messaging", 
		"language": "und", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook.json",
						"templating": {
							"uuid": "1ae96956-4b34-433e-8d1a-f05fe6923d6d",
							"template": {
								"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
								"name": "welcome"
							},
							"variables": ["@contact.name"]
						}
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`), migrated)

	// 13.3 > 13.4
	migrated, err = goflow.MigrateDefinition(rt.Config, migrated, semver.MustParse("13.4.0"))
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.4.0", 
		"type": "messaging", 
		"language": "und", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook.json",
						"templating": {
							"template": {
								"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
								"name": "welcome"
							},
							"components": [
								{
									"uuid": "e7187099-7d38-4f60-955c-325957214c42", 
									"name": "body", 
									"params": ["@contact.name"]
								}
							]
						}
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`), migrated)

	// 13.4 > 13.5
	migrated, err = goflow.MigrateDefinition(rt.Config, migrated, semver.MustParse("13.5.0"))
	assert.NoError(t, err)
	test.AssertEqualJSON(t, []byte(`{
		"uuid": "502c3ee4-3249-4dee-8e71-c62070667d52", 
		"name": "New", 
		"spec_version": "13.5.0", 
		"type": "messaging", 
		"language": "und", 
		"nodes": [
			{
				"uuid": "d26486b1-193d-4512-85f0-c6db696f1e1c",
				"actions": [
					{
						"uuid": "82a1de5f-af1a-45ef-8511-4d60c160e486",
						"type": "send_msg",
						"text": "Hello @webhook.json",
						"template": {
							"uuid": "641b8b05-082a-497e-bf63-38aa48b1f0c4",
							"name": "welcome"
						},
						"template_variables": [
							"@contact.name"
						]
					}
				],
				"exits": [
					{
						"uuid": "fdd370e0-ffa9-48b3-8148-b9241d74fc72"
					}
				]
			}
		]
	}`), migrated)
}
