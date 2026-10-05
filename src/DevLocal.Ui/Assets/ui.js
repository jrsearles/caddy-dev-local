(function () {
    const labels = {
        system: 'Switch to light theme',
        light: 'Switch to dark theme',
        dark: 'Switch to system theme',
    };

    function current() {
        const value = document.documentElement.getAttribute('data-theme');
        return value === 'light' || value === 'dark' ? value : 'system';
    }

    function apply(theme) {
        const root = document.documentElement;
        try {
            if (theme === 'system') {
                root.removeAttribute('data-theme');
                localStorage.removeItem('devlocal-theme');
            } else {
                root.setAttribute('data-theme', theme);
                localStorage.setItem('devlocal-theme', theme);
            }
        } catch (e) { }
        return theme;
    }

    function legacyCopy(text) {
        const area = document.createElement('textarea');
        area.value = text;
        area.style.cssText = 'position:fixed;opacity:0';
        document.body.appendChild(area);
        area.select();
        try { document.execCommand('copy'); } catch (e) { }
        document.body.removeChild(area);
        return Promise.resolve(true);
    }

    window.devlocalUi = {
        theme: function () { return current(); },
        cycleTheme: function () {
            return apply(current() === 'system' ? 'light' : current() === 'light' ? 'dark' : 'system');
        },
        themeLabel: function (theme) { return labels[theme] || labels.system; },
        copy: function (text) {
            if (navigator.clipboard && window.isSecureContext)
                return navigator.clipboard.writeText(text).then(function () { return true; }, function () { return legacyCopy(text); });
            return legacyCopy(text);
        },
        scrollTo: function (id) {
            const target = document.getElementById(id);
            if (target) target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        },
        focusSearch: function () {
            const input = document.getElementById('devlocal-search');
            if (input) { input.focus(); input.select(); }
        },
    };
})();