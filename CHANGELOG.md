# Changelog

**ssp-backend**

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
and [human-readable changelog](https://keepachangelog.com/en/1.0.0/).

The current role maintainer is the SBB Cloud Platform Team.

## [Master](https://github.com/SchweizerischeBundesbahnen/ssp-backend/commits/master) - unreleased

### Added


## [3.9.0](https://github.com/SchweizerischeBundesbahnen/ssp-backend/compare/v3.9.0...v3.8.1) - 29.07.2020

### Added

- Function `getJobTemplateDetails()` and API route `/tower/job_templates/<id>/getDetails` (GET)
  added, to get survey specs from Ansible Tower templates.
- The "functional account" (an additional project admin for Openshift) is now configurable and not
  hardcoded into the go file.
- New tests for the validateProjectPermissions() function from openshift/project.go

## [3.8.1]

Changes were not tracked...
