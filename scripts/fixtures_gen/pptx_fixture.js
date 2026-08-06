// The deck fixture, built with pptxgenjs as the pptx skill prescribes.
//
// Two slides rather than one, because a single-slide deck cannot show the failure
// this fixture is here to catch: a PDF made FROM slides defeats every extractor
// tested, since the sentence breaks across separate text boxes and no extractor
// can know they were one sentence. Keeping the canary inside ONE box on slide 2
// is the case that must work; the split on slide 1 is the case we know does not,
// and it is documented rather than pretended away.

const pptxgen = require('pptxgenjs');

const CANARY =
  'La sovranita appartiene al popolo che la esercita nelle forme e nei limiti della Costituzione.';

const pptx = new pptxgen();
pptx.layout = 'LAYOUT_16x9';

const split = pptx.addSlide();
split.addText('Corpus di prova Aura', { x: 0.5, y: 0.4, w: 9, h: 0.8, fontSize: 28, bold: true });
split.addText('La sovranita appartiene al popolo', { x: 0.5, y: 1.6, w: 4.2, h: 0.6, fontSize: 16 });
split.addText('che la esercita nelle forme e nei limiti della Costituzione.', {
  x: 5.0, y: 1.6, w: 4.2, h: 0.6, fontSize: 16,
});

const whole = pptx.addSlide();
whole.addText('Principi fondamentali', { x: 0.5, y: 0.4, w: 9, h: 0.8, fontSize: 28, bold: true });
whole.addText(CANARY, { x: 0.5, y: 1.6, w: 9, h: 1.2, fontSize: 16 });

const out = process.argv[2] || 'sample.pptx';
pptx.writeFile({ fileName: out }).then(() =>
  console.log(`${out}: 2 slide, canarino intero sulla seconda`),
);
