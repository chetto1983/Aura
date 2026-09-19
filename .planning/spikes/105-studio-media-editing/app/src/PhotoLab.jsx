import { useRef, useState } from 'react';
import FilerobotImageEditor, { TABS, TOOLS } from 'react-filerobot-image-editor';
import { StyleSheetManager } from 'styled-components';
import isPropValid from '@emotion/is-prop-valid';
import { log } from './log.js';
import { IT } from './it.js';

// styled-components 6 stopped filtering unknown props, and Filerobot's styled components pass
// theirs (showTabsDrawer, isPhoneScreen, active…) straight to the DOM. This restores v5's filter.
const forwardDomProp = (prop, target) => (typeof target === 'string' ? isPropValid(prop) : true);

const SAMPLES = ['studio-1024.png', 'studio-1024.webp', 'photo-12mp.jpg', 'photo-exif6.jpg'];

// Filerobot defaults that a self-hosted cockpit must override, both measured in this spike:
// useBackendTranslations=true fetches strings from i18n-fastly.ultrafast.io on every mount, and
// savingPixelRatio=4 renders the saved image at 4x the preview's pixel density.
const SELF_HOSTED = { useBackendTranslations: false, savingPixelRatio: 1, previewPixelRatio: 1 };

function base64Bytes(b64) {
  const s = b64.split(',')[1] ?? '';
  return Math.floor((s.length * 3) / 4) - (s.endsWith('==') ? 2 : s.endsWith('=') ? 1 : 0);
}

export default function PhotoLab() {
  const [source, setSource] = useState('/studio-1024.png');
  const [cfg, setCfg] = useState({ backend: false, language: 'en', ratio: 1 });
  const [saved, setSaved] = useState(null);
  const imgDataRef = useRef({});

  function onSave(imageData, designState) {
    const meta = {
      name: imageData.fullName,
      mimeType: imageData.mimeType,
      width: imageData.width,
      height: imageData.height,
      bytes: imageData.imageBase64 ? base64Bytes(imageData.imageBase64) : null,
      filter: designState.filter,
      finetunes: designState.finetunes,
      adjustments: designState.adjustments,
      annotations: Object.keys(designState.annotations ?? {}).length,
    };
    log('photo', 'saved', meta);
    setSaved({ url: imageData.imageBase64, meta });
  }

  // Playwright calls the same save function the Save button does, with a type and ratio.
  window.__spike = window.__spike ?? {};
  window.__spike.photoSave = (name, extension, pixelRatio) => {
    const t0 = performance.now();
    const { imageData, designState } = imgDataRef.current({ name, extension }, pixelRatio);
    const ms = Math.round(performance.now() - t0);
    onSave(imageData, designState);
    return { ms, width: imageData.width, height: imageData.height, mimeType: imageData.mimeType, bytes: base64Bytes(imageData.imageBase64), base64: imageData.imageBase64 };
  };
  window.__spike.photoLoad = (name) => setSource(`/${name}`);

  return (
    <section className="lab">
      <div className="row">
        {SAMPLES.map((s) => (
          <button key={s} onClick={() => setSource(`/${s}`)}>{s}</button>
        ))}
        <label className="file">
          La tua foto… <input type="file" accept="image/*" onChange={(e) => e.target.files[0] && setSource(URL.createObjectURL(e.target.files[0]))} />
        </label>
        <label>
          <input type="checkbox" checked={cfg.backend} onChange={(e) => setCfg((c) => ({ ...c, backend: e.target.checked }))} /> traduzioni dal server Scaleflex
        </label>
        <select value={cfg.language} onChange={(e) => setCfg((c) => ({ ...c, language: e.target.value }))}>
          <option value="en">en</option>
          <option value="it">it</option>
          <option value="it-aura">it (tabella nostra)</option>
        </select>
      </div>
      <div className="editor">
        <StyleSheetManager shouldForwardProp={forwardDomProp}>
        <FilerobotImageEditor
          key={`${source}|${cfg.backend}|${cfg.language}`}
          source={source}
          {...SELF_HOSTED}
          useBackendTranslations={cfg.backend}
          language={cfg.language === 'it-aura' ? 'it' : cfg.language}
          translations={cfg.language === 'it-aura' ? IT : undefined}
          onSave={onSave}
          getCurrentImgDataFnRef={imgDataRef}
          defaultSavedImageType="png"
          tabsIds={[TABS.ADJUST, TABS.FINETUNE, TABS.FILTERS, TABS.ANNOTATE, TABS.WATERMARK, TABS.RESIZE]}
          defaultTabId={TABS.ADJUST}
          defaultToolId={TOOLS.CROP}
          Text={{ text: 'Aura' }}
          Rotate={{ angle: 90, componentType: 'slider' }}
        />
        </StyleSheetManager>
      </div>
      {saved && (
        <div className="saved">
          <img src={saved.url} alt="salvata" />
          <pre>{JSON.stringify(saved.meta, null, 1)}</pre>
        </div>
      )}
    </section>
  );
}
