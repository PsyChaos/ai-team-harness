# Testing Policy

Tests protect meaningful behavior; test count is not a goal.

Add/change tests when behavior changed, a defect needs regression coverage, a meaningful boundary is introduced, or risk justifies it.

Avoid:

- huge near-duplicate parameter matrices without distinct risk
- framework-internal tests
- trivial getter/setter tests
- brittle implementation-detail tests
- snapshot proliferation without semantic value
- mocks that only prove mocks

Start with targeted validation. Broaden when risk, shared infrastructure, failures or repository policy justify it. Do not rerun an expensive entire suite after every tiny edit without evidence that it is needed.
