# Password File Test Data

This directory contains test password files (.pwd) used for testing the password file manager functionality.

## Test Files

- `valid_password.pwd` - Contains a valid password for testing
- `empty_password.pwd` - Empty password file for testing error handling
- `whitespace_password.pwd` - Password file with whitespace for testing trimming
- `utf8_password.pwd` - Password file with UTF-8 characters
- `long_password.pwd` - Password file with maximum allowed length
- `oversized_password.pwd` - Password file exceeding size limits

## Usage

These files are used by the password file manager tests to verify:
- Password file detection and reading
- Error handling for various file conditions
- UTF-8 encoding support
- File size validation
- Whitespace trimming

## Security Note

These are test files only and do not contain real passwords or sensitive data.