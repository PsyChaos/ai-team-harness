package ghgateway

import "fmt"

const itemFragment = `
fragment HarnessProjectItem on ProjectV2Item {
  id
  isArchived
  content {
    __typename
    ... on Issue {
      id number url title body state
      repository { nameWithOwner }
    }
  }
  fieldValues(first: 50) {
    nodes {
      __typename
      ... on ProjectV2ItemFieldSingleSelectValue {
        name field { ... on ProjectV2FieldCommon { name } }
      }
      ... on ProjectV2ItemFieldTextValue {
        text field { ... on ProjectV2FieldCommon { name } }
      }
      ... on ProjectV2ItemFieldNumberValue {
        number field { ... on ProjectV2FieldCommon { name } }
      }
      ... on ProjectV2ItemFieldIterationValue {
        title field { ... on ProjectV2FieldCommon { name } }
      }
    }
    pageInfo { hasNextPage }
  }
}
`

const blockersQuery = `
query HarnessProjectBlockers($ids: [ID!]!) {
  nodes(ids: $ids) {
    ... on Issue {
      id
      blockedBy(first: 100) {
        nodes { number url title state repository { nameWithOwner } }
        pageInfo { hasNextPage }
      }
    }
  }
}
`

func projectQuery(root string) string {
	return fmt.Sprintf(`query HarnessProjectItems($login: String!, $number: Int!, $cursor: String) {
 %s(login: $login) { projectV2(number: $number) {
 items(first: 100, after: $cursor) { nodes { ...HarnessProjectItem } pageInfo { hasNextPage endCursor } }
 } }
 }`, root) + itemFragment
}
