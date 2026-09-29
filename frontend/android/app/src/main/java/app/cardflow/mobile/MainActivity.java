package app.cardflow.mobile;

import android.content.res.Configuration;
import android.graphics.Color;
import android.os.Bundle;
import androidx.core.graphics.Insets;
import androidx.core.view.ViewCompat;
import androidx.core.view.WindowCompat;
import androidx.core.view.WindowInsetsCompat;
import androidx.core.view.WindowInsetsControllerCompat;
import com.getcapacitor.Bridge;
import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);

        // Edge-to-edge: the app's own background draws behind the status bar
        // and gesture nav bar. Both bars stay visible and fully functional
        // (swipe down for notifications, gesture nav, etc.) — only their
        // opaque background goes away so the app doesn't have a black seam
        // across the top/bottom.
        WindowCompat.setDecorFitsSystemWindows(getWindow(), false);
        getWindow().setStatusBarColor(Color.TRANSPARENT);
        getWindow().setNavigationBarColor(Color.TRANSPARENT);

        applySystemBarIconAppearance();

        // Android WebView's CSS env(safe-area-inset-*) support is
        // inconsistent, so compute the real system-bar insets natively and
        // hand them to the page as CSS custom properties instead of relying
        // on env() alone (index.html falls back to env() for web/iOS).
        ViewCompat.setOnApplyWindowInsetsListener(getWindow().getDecorView(), (view, insets) -> {
            Insets systemBars = insets.getInsets(WindowInsetsCompat.Type.systemBars());
            injectSafeAreaInsets(systemBars);
            return insets;
        });
    }

    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        // The phone's system dark/light setting can change while the app is
        // running (or be different than it was at launch) — keep the status
        // and nav bar icon color matched to it.
        applySystemBarIconAppearance();
    }

    private void applySystemBarIconAppearance() {
        int nightModeFlags = getResources().getConfiguration().uiMode & Configuration.UI_MODE_NIGHT_MASK;
        boolean isNightMode = nightModeFlags == Configuration.UI_MODE_NIGHT_YES;

        WindowInsetsControllerCompat controller =
                new WindowInsetsControllerCompat(getWindow(), getWindow().getDecorView());
        // Light background -> dark icons; dark background -> light icons.
        controller.setAppearanceLightStatusBars(!isNightMode);
        controller.setAppearanceLightNavigationBars(!isNightMode);
    }

    private void injectSafeAreaInsets(Insets insets) {
        float density = getResources().getDisplayMetrics().density;
        int top = Math.round(insets.top / density);
        int bottom = Math.round(insets.bottom / density);
        int left = Math.round(insets.left / density);
        int right = Math.round(insets.right / density);
        String js = "document.documentElement.style.setProperty('--cf-safe-top','" + top + "px');"
                + "document.documentElement.style.setProperty('--cf-safe-bottom','" + bottom + "px');"
                + "document.documentElement.style.setProperty('--cf-safe-left','" + left + "px');"
                + "document.documentElement.style.setProperty('--cf-safe-right','" + right + "px');";

        Bridge bridge = getBridge();
        if (bridge != null && bridge.getWebView() != null) {
            bridge.getWebView().post(() -> bridge.getWebView().evaluateJavascript(js, null));
        }
    }
}
