package internal

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/stretchr/testify/mock"
)

type MockPrompter struct{ mock.Mock }

func (p *MockPrompter) Input(prompt, defaultValue string) (string, error) {
	args := p.Called(prompt, defaultValue)
	return args.String(0), args.Error(1)
}

func (p *MockPrompter) Select(prompt, defaultValue string, options []string) (int, error) {
	args := p.Called(prompt, defaultValue, options)
	return args.Int(0), args.Error(1)
}

func (p *MockPrompter) MultiSelect(prompt string, defaultValues, options []string) ([]int, error) {
	args := p.Called(prompt, defaultValues, options)
	return args.Get(0).([]int), args.Error(1)
}

func (p *MockPrompter) Confirm(prompt string, defaultValue bool) (bool, error) {
	args := p.Called(prompt, defaultValue)
	return args.Bool(0), args.Error(1)
}

func NewMockPrompter() *MockPrompter {
	return &MockPrompter{}
}

type TestRootCmdOptions struct {
	Mock     *mock.Mock
	Prompter *MockPrompter
	In       *bytes.Buffer
	Out      *bytes.Buffer
	Err      *bytes.Buffer
}

type TestFile struct {
	Contents string
}

func (file *TestFile) Reader() io.Reader {
	return bytes.NewBufferString(file.Contents)
}

func (file *TestFile) Close() error {
	return nil
}

func (testOpts *TestRootCmdOptions) MockTemplateHole(team string, name string, value string) *mock.Call {
	return testOpts.Prompter.Mock.On("Input", fmt.Sprintf("%s: %s", team, name), "").Return(value, nil)
}

func (testOpts *TestRootCmdOptions) MockCodeowners(codeownersContent []string) {
	testOpts.Mock.On("ReadFile", ".github/CODEOWNERS").
		Return(&TestFile{strings.Join(codeownersContent, "\n")}, nil)
}

func (testOpts *TestRootCmdOptions) MockWorkingDirectory(files []string) {
	testOpts.Mock.
		On("GitExec", []string{"--no-pager", "diff", "--name-only"}).
		Return([]byte(strings.Join(files, "\n")), nil)
}

func NewTestRootOpts() *TestRootCmdOptions {
	return &TestRootCmdOptions{
		In:       bytes.NewBuffer([]byte{}),
		Out:      bytes.NewBuffer([]byte{}),
		Err:      bytes.NewBuffer([]byte{}),
		Mock:     &mock.Mock{},
		Prompter: NewMockPrompter(),
	}
}
