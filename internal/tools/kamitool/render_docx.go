package kamitool

import (
        "baliance.com/gooxml/color"
        "baliance.com/gooxml/document"
        "baliance.com/gooxml/measurement"
        "baliance.com/gooxml/schema/soo/wml"
)

// docxConfig controls the document-wide style for a DOCX render.
type docxConfig struct {
        fontFamily   string
        fontSize     float64 // points
        marginTop    float64 // inches
        marginBottom  float64
        marginLeft    float64
        marginRight   float64
        lineSpacing   float64 // multiple (2.0 = double-spaced)
        headingColor  string  // hex without #
        bodyColor     string
}

func kamiDOCXConfig() docxConfig {
        return docxConfig{
                fontFamily:   fontDocxPrimary,
                fontSize:     11,
                marginTop:    1.0, marginBottom: 1.0, marginLeft: 1.0, marginRight: 1.0,
                lineSpacing:  1.15,
                headingColor: "1B365D",
                bodyColor:    "2C2C2C",
        }
}

func essayDOCXConfig() docxConfig {
        return docxConfig{
                fontFamily:   fontEssayDocx,
                fontSize:     12,
                marginTop:    1.0, marginBottom: 1.0, marginLeft: 1.0, marginRight: 1.0,
                lineSpacing:  2.0,
                headingColor: "000000",
                bodyColor:    "000000",
        }
}

func newDOCX(cfg docxConfig) *document.Document {
        doc := document.New()

        // Page margins via the body section's wml struct.
        section := doc.BodySection()
        sectPr := section.X()
        if sectPr.PgMar == nil {
                sectPr.PgMar = wml.NewCT_PageMar()
        }
        toTwips := func(inches float64) int64 {
                return int64(inches * 72 * 20) // inches → points → twips
        }
        topTwips := toTwips(cfg.marginTop)
        botTwips := toTwips(cfg.marginBottom)
        leftTwips := uint64(toTwips(cfg.marginLeft))
        rightTwips := uint64(toTwips(cfg.marginRight))
        hdrTwips := uint64(toTwips(0.5))
        ftrTwips := uint64(toTwips(0.5))
        sectPr.PgMar.TopAttr.Int64 = &topTwips
        sectPr.PgMar.BottomAttr.Int64 = &botTwips
        sectPr.PgMar.LeftAttr.ST_UnsignedDecimalNumber = &leftTwips
        sectPr.PgMar.RightAttr.ST_UnsignedDecimalNumber = &rightTwips
        sectPr.PgMar.HeaderAttr.ST_UnsignedDecimalNumber = &hdrTwips
        sectPr.PgMar.FooterAttr.ST_UnsignedDecimalNumber = &ftrTwips
        sectPr.PgMar.GutterAttr.ST_UnsignedDecimalNumber = new(uint64) // 0

        // Default font + size on the document defaults. gooxml doesn't
        // expose SearchStyleById, so we set the DocDefaults directly.
        doc.Styles.InitializeDefault()
        if defaults := doc.Styles.X().DocDefaults; defaults != nil && defaults.RPrDefault != nil && defaults.RPrDefault.RPr != nil {
                rPr := defaults.RPrDefault.RPr
                // Font family.
                if rPr.RFonts == nil {
                        rPr.RFonts = wml.NewCT_Fonts()
                }
                rPr.RFonts.AsciiAttr = &cfg.fontFamily
                rPr.RFonts.HAnsiAttr = &cfg.fontFamily
                rPr.RFonts.EastAsiaAttr = &cfg.fontFamily
                // Font size (in half-points).
                sz := uint64(cfg.fontSize * 2) // half-points
                if rPr.Sz == nil {
                        rPr.Sz = wml.NewCT_HpsMeasure()
                }
                rPr.Sz.ValAttr.ST_UnsignedDecimalNumber = &sz
                if rPr.SzCs == nil {
                        rPr.SzCs = wml.NewCT_HpsMeasure()
                }
                rPr.SzCs.ValAttr.ST_UnsignedDecimalNumber = &sz
        }

        // Line spacing on the document default paragraph properties.
        if cfg.lineSpacing > 0 && doc.Styles.X().DocDefaults != nil && doc.Styles.X().DocDefaults.PPrDefault != nil {
                pPr := doc.Styles.X().DocDefaults.PPrDefault.PPr
                if pPr == nil {
                        pPr = wml.NewCT_PPrGeneral()
                        doc.Styles.X().DocDefaults.PPrDefault.PPr = pPr
                }
                if pPr.Spacing == nil {
                        pPr.Spacing = wml.NewCT_Spacing()
                }
                lineVal := int64(cfg.lineSpacing * 240) // 240 twips = single, 480 = double
                pPr.Spacing.LineAttr = &wml.ST_SignedTwipsMeasure{}
                pPr.Spacing.LineAttr.Int64 = &lineVal
                pPr.Spacing.LineRuleAttr = wml.ST_LineSpacingRuleAuto
        }

        return doc
}

func addHeading(doc *document.Document, cfg docxConfig, text string, level int) {
        para := doc.AddParagraph()
        run := para.AddRun()
        run.AddText(text)
        run.Properties().SetFontFamily(cfg.fontFamily)

        switch level {
        case 1:
                run.Properties().SetSize(measurement.Distance(cfg.fontSize * 1.6 * measurement.Point))
                run.Properties().SetBold(true)
        case 2:
                run.Properties().SetSize(measurement.Distance(cfg.fontSize * 1.3 * measurement.Point))
                run.Properties().SetBold(true)
        case 3:
                run.Properties().SetSize(measurement.Distance(cfg.fontSize * 1.15 * measurement.Point))
                run.Properties().SetBold(true)
        }
        if cfg.headingColor != "" {
                run.Properties().SetColor(color.FromHex(cfg.headingColor))
        }
        para.Properties().SetSpacing(0, measurement.Distance(6*measurement.Point))
}

func addBodyParagraph(doc *document.Document, cfg docxConfig, text string) {
        para := doc.AddParagraph()
        run := para.AddRun()
        run.AddText(text)
        run.Properties().SetFontFamily(cfg.fontFamily)
        run.Properties().SetSize(measurement.Distance(cfg.fontSize * measurement.Point))
        if cfg.bodyColor != "" {
                run.Properties().SetColor(color.FromHex(cfg.bodyColor))
        }
}

func addBullet(doc *document.Document, cfg docxConfig, text string) {
        para := doc.AddParagraph()
        para.SetStyle("ListBullet")
        run := para.AddRun()
        run.AddText(text)
        run.Properties().SetFontFamily(cfg.fontFamily)
        run.Properties().SetSize(measurement.Distance(cfg.fontSize * measurement.Point))
        if cfg.bodyColor != "" {
                run.Properties().SetColor(color.FromHex(cfg.bodyColor))
        }
}

func saveDOCX(doc *document.Document, path string) error {
        return doc.SaveToFile(path)
}
