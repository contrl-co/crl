package crl

import "github.com/contrl-co/crl/internal/crypto"

type LanguageCompilation struct {
	SourceHash    string        `json:"source_hash"`
	Syntax        SyntaxTree    `json:"syntax"`
	Document      Document      `json:"document"`
	Semantic      SemanticModel `json:"semantic"`
	IR            IRProgram     `json:"ir"`
	Bundle        Bundle        `json:"bundle"`
	CanonicalText string        `json:"canonical_text"`
	Hash          string        `json:"hash"`
}

func CompileLanguage(source string) (LanguageCompilation, error) {
	syntax, err := Parse(source)
	if err != nil {
		return LanguageCompilation{}, err
	}
	document, err := BuildDocument(syntax)
	if err != nil {
		return LanguageCompilation{}, err
	}
	bundle, err := normalizeBundle(document.Bundle())
	if err != nil {
		return LanguageCompilation{}, err
	}
	semantic, err := analyzeNormalizedBundle(bundle)
	if err != nil {
		return LanguageCompilation{}, err
	}
	ir := LowerBundle(bundle, semantic)
	hash, err := crypto.Digest(bundle)
	if err != nil {
		return LanguageCompilation{}, err
	}
	return LanguageCompilation{
		SourceHash:    crypto.DigestBytes([]byte(source)),
		Syntax:        syntax,
		Document:      document,
		Semantic:      semantic,
		IR:            ir,
		Bundle:        bundle,
		CanonicalText: canonicalBundleText(bundle),
		Hash:          hash,
	}, nil
}

func ParseDocument(source string) (Document, error) {
	syntax, err := Parse(source)
	if err != nil {
		return Document{}, err
	}
	return BuildDocument(syntax)
}
