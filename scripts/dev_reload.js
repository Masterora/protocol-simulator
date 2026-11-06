// Injected only by a devassets build; each native process uses its own Go bridge.
(() => {
  let revision = __PRS_INITIAL_REVISION__,
    busy = false,
    reported = false;
  const editing = () => {
    const el = document.activeElement;
    return !!el && (el.matches('input,textarea,select') || el.isContentEditable);
  };
  async function poll() {
    const app = window.go?.main?.App;
    if (busy || !app?.GetDevelopmentState || !document.body) return;
    busy = true;
    try {
      if (!reported) {
        await app.ReportDevelopmentReload?.('page', revision.code);
        reported = true;
      }
      const next = await app.GetDevelopmentState();
      if (!next.enabled) return;
      if (next.code !== revision.code) {
        if (!editing()) window.location.reload();
        return;
      }
      if (next.styles !== revision.styles) {
        const links = [...document.querySelectorAll('link[rel="stylesheet"]')];
        await Promise.all(
          links.map(
            (link) =>
              new Promise((resolve, reject) => {
                const updated = link.cloneNode();
                const url = new URL(link.href, location.href);
                url.searchParams.set('prs-dev', next.styles);
                updated.href = url.href;
                updated.onload = () => {
                  link.remove();
                  resolve();
                };
                updated.onerror = () => {
                  updated.remove();
                  reject(Error('样式刷新失败'));
                };
                link.after(updated);
              }),
          ),
        );
        revision = next;
        await app.ReportDevelopmentReload?.('styles', next.styles);
      }
    } catch (error) {
      console.debug('开发刷新等待重试', error);
    } finally {
      busy = false;
    }
  }
  setInterval(poll, 700);
})();
