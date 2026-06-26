# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 Robin Jarry

TREX_CLIENT_VERSION ?= $(shell git describe --long --abbrev=8 --dirty 2>/dev/null || echo v1.0.0)

GO ?= go
V ?= 0
ifeq ($V,1)
Q =
else
Q = @
endif

license_exclude = ':!:*.md' ':!:*.asc' ':!:LICENSE' ':!:.*'

.PHONY: lint
lint:
	@echo '[license-check]'
	$Q ! git --no-pager grep -LF 'SPDX-License-Identifier: Apache-2.0' -- $(license_exclude) || { \
		echo 'error: above files are missing license'; \
		exit 1; \
	}
	$Q ! git --no-pager grep -LF 'Copyright (c)' -- $(license_exclude) || { \
		echo 'error: above files are missing copyright notice'; \
		exit 1; \
	}
	@echo '[white-space]'
	$Q git ls-files | xargs devtools/check-whitespace
	@echo '[codespell]'
	$Q codespell *

REVISION_RANGE ?= @{u}..

.PHONY: check-patches
check-patches:
	$Q devtools/check-patches $(REVISION_RANGE)

.PHONY: git-config
git-config:
	@rm -f .git/hooks/commit-msg*
	ln -s ../../devtools/commit-msg .git/hooks/commit-msg

.PHONY: tag-release
tag-release:
	@cur_version=`sed -En 's/TREX_CLIENT_VERSION .* \|\| echo v([0-9].*)\>\)$$/\1/p' Makefile` && \
	next_version=`echo $$cur_version | awk -F. -v OFS=. '{$$(NF) += 1; print}'` && \
	read -rp "next version ($$next_version)? " n && \
	if [ -n "$$n" ]; then next_version="$$n"; fi && \
	set -xe && \
	sed -i "s/\<v$$cur_version\>/v$$next_version/" Makefile && \
	git commit -sm "trex-client: release v$$next_version" -m "`devtools/git-stats v$$cur_version..`" Makefile && \
	git tag -sm "`devtools/git-stats v$$cur_version..HEAD^`" "v$$next_version"
