// The Word fixture, built with docx-js as the docx skill prescribes.
//
// The paragraph is JUSTIFIED on purpose. Justification is what makes a renderer
// stretch inter-word spacing, and it is what broke exact-phrase search before:
// MarkItDown emitted 49.8 double spaces per 1k characters on a justified page
// while producing MORE characters than a clean extractor, so a character-count
// check passed and the lexical leg of hybrid retrieval quietly stopped matching.
//
// The CANARY sentence must survive verbatim through this file and through every
// format LibreOffice fans it out to.

const fs = require('fs');
const { Document, Packer, Paragraph, TextRun, HeadingLevel, AlignmentType } = require('docx');

const CANARY =
  'La sovranita appartiene al popolo che la esercita nelle forme e nei limiti della Costituzione.';

const doc = new Document({
  sections: [
    {
      children: [
        new Paragraph({
          text: 'Principi fondamentali del corpus di prova',
          heading: HeadingLevel.HEADING_1,
        }),
        new Paragraph({
          alignment: AlignmentType.JUSTIFIED,
          children: [new TextRun({ text: CANARY, font: 'Times New Roman', size: 24 })],
        }),
        new Paragraph({
          alignment: AlignmentType.JUSTIFIED,
          children: [
            new TextRun({
              text:
                'Il canarino qui sopra deve sopravvivere verbatim a ogni estrattore della catena. ' +
                'Se un estrattore riflusce il testo la frase resta leggibile a un occhio umano ma ' +
                'smette di essere trovabile con una ricerca per frase esatta, e nessun errore viene ' +
                'sollevato da nessuna parte: il documento risulta indicizzato e semplicemente non ' +
                'risponde piu. Questo paragrafo esiste per rendere la pagina abbastanza larga da ' +
                'costringere la giustificazione ad allargare gli spazi fra le parole.',
              font: 'Times New Roman',
              size: 24,
            }),
          ],
        }),
      ],
    },
  ],
});

Packer.toBuffer(doc).then((buf) => {
  const out = process.argv[2] || 'sample.docx';
  fs.writeFileSync(out, buf);
  console.log(`${out}: ${buf.length} byte, paragrafo giustificato con il canarino`);
});
