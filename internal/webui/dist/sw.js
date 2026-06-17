const CACHE_NAME="aura-precache-a470a24910b1ed8c";
const PRECACHE=[{"url":"/apple-touch-icon.png","revision":"c513708f0a839146"},{"url":"/assets/AppShell-DxpAA6KG.js","revision":"0ac18123f321db2d"},{"url":"/assets/index-C0hoUG7i.css","revision":"1ce4fb87ddb424b6"},{"url":"/assets/index-ClI2ss51.js","revision":"b854d2c8249e9dd1"},{"url":"/assets/LoginPage-DzgKccWa.js","revision":"6c602026c042adb2"},{"url":"/assets/NotFoundView-ByPUbkAp.js","revision":"812d7bba107b884d"},{"url":"/assets/ThemeSwitcher-BWPNzOE1.js","revision":"9b1b89d1f5dbd867"},{"url":"/assets/useTranslation-BqFMNT4W.js","revision":"558d594752aa5bb2"},{"url":"/favicon.svg","revision":"efe40f6f3218842d"},{"url":"/index.html","revision":"app-shell"},{"url":"/logo.png","revision":"5afa3c5decd2b269"},{"url":"/manifest.webmanifest","revision":"963b1ffd5fd5cf48"},{"url":"/pwa-192.png","revision":"8cb445cd2e408732"},{"url":"/pwa-512.png","revision":"5afa3c5decd2b269"},{"url":"/pwa-maskable-512.png","revision":"5afa3c5decd2b269"},{"url":"/registerSW.js","revision":"4e5d4d2e5890e9d2"}];
self.addEventListener('install',(event)=>{
  event.waitUntil(caches.open(CACHE_NAME).then((cache)=>Promise.all(PRECACHE.map((entry)=>cache.add(entry.url).catch(()=>undefined)))).then(()=>self.skipWaiting()));
});
self.addEventListener('activate',(event)=>{
  event.waitUntil(caches.keys().then((keys)=>Promise.all(keys.filter((key)=>key.startsWith('aura-precache-')&&key!==CACHE_NAME).map((key)=>caches.delete(key)))).then(()=>self.clients.claim()));
});
self.addEventListener('fetch',(event)=>{
  const request=event.request;
  if(request.method!=='GET') return;
  const url=new URL(request.url);
  if(url.origin!==self.location.origin) return;
  if(request.mode==='navigate'){
    event.respondWith(fetch(request).catch(()=>caches.match('/index.html')));
    return;
  }
  event.respondWith(caches.match(request).then((cached)=>cached||fetch(request)));
});
