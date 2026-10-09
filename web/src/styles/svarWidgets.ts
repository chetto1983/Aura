// Every SVAR widget's stylesheet, in the one order that keeps the cockpit on its own origin.
//
// The packages' full CSS declares Open Sans and Roboto as @font-face rules served from
// cdn.svar.dev, and svar.css redeclares the same faces against this origin. Between faces that
// agree on family, style and weight the LATER one wins, so svar.css must come after every
// package sheet. Imported from each workspace on its own, svar.css became a chunk the two
// widgets share, which the bundler loads before either widget's CSS: the CDN faces won again
// and the video studio's no-other-origin check caught Roboto leaving the appliance
// (CI 2026-10-09, the board's first push). One module imported by both keeps the three sheets
// in one chunk, in this order.
import '@svar-ui/react-filemanager/all.css';
import '@svar-ui/react-kanban/all.css';
import './svar.css';
