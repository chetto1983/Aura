// The parity test imports Filerobot's own English table, which the package ships without types.
declare module 'react-filerobot-image-editor/lib/context/defaultTranslations' {
  const translations: Readonly<Record<string, string>>;
  export default translations;
}
