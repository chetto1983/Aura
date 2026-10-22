"""Where each formula comes from. Every tool result cites one of these keys."""

REFERENCES: dict[str, str] = {
    "sharpe": "Sharpe ratio, annualised: mean(r - rf) / std(r) * sqrt(252). "
    "Standard error under Gaussian returns: sqrt((1 + SR^2/2) / n), Lo (2002), as restated in "
    "arXiv:1906.00573 and arXiv:2302.08829.",
    "psr": "Probabilistic Sharpe Ratio, Bailey & Lopez de Prado (2012), J. of Risk 15(2): "
    "PSR = Phi((SR - SR*) * sqrt(T - 1) / sqrt(1 - g3*SR + (g4 - 1)/4 * SR^2)), SR per period, "
    "g3 skewness, g4 raw kurtosis. Reduces to Lo's Gaussian standard error when g3=0, g4=3. "
    "Applied on arXiv in arXiv:1910.05555 (Sec. VI.C).",
    "sortino": "Sortino ratio: annualised mean excess return over annualised downside deviation "
    "sqrt(mean(min(r - MAR, 0)^2)).",
    "omega": "Omega ratio, Keating & Shadwick (2002), as defined in arXiv:1911.10254 eq. (8): "
    "Omega(theta) = E[max(r - theta, 0)] / E[max(theta - r, 0)].",
    "max_drawdown": "Maximum drawdown: largest peak-to-trough loss of the cumulative return path, "
    "arXiv:1404.7493.",
    "cdar": "Conditional Drawdown-at-Risk, Chekhlov, Uryasev & Zabarankin: the average of the "
    "worst (1 - alpha) share of drawdowns, i.e. the CVaR of the drawdown series, as described in "
    "arXiv:1908.08684 (Sec. 2.2.1).",
    "calmar": "Calmar ratio: annualised return (CAGR) / |maximum drawdown|.",
    "var_historical": "Historical VaR: empirical alpha-quantile of returns. Expected shortfall: "
    "mean return below that quantile. Definitions and backtesting properties: arXiv:1507.00244.",
    "var_cornish_fisher": "Modified (Cornish-Fisher) VaR, Favre & Galeano (2002), Boudt, Peterson & "
    "Croux (2008), J. of Risk 11(2). Not an arXiv source: z_cf = z + (z^2-1)S/6 + (z^3-3z)K/24 "
    "- (2z^3-5z)S^2/36, K excess kurtosis.",
    "beta": "Beta: cov(r, r_m) / var(r_m) on aligned daily returns.",
    "beta_adjusted": "Bloomberg-style beta: raw beta on the last 104 Friday-to-Friday returns (two years, the default "
    "window of Bloomberg's BETA screen), adjusted as 0.67 x raw + 0.33 x 1, the shrinkage toward the market Blume "
    "(1971, J. of Finance 26(1)) measured; weights as documented by university guides to the BETA screen (BYU, "
    "Babson). Bloomberg's own figure can differ with its data, its index and its return definition.",
    "capm": "Jensen's alpha = (mean(r - rf) - beta x mean(r_m - rf)) x 252; Treynor = mean(r - rf) x 252 / beta "
    "(null when beta <= 0); Modigliani & Modigliani (1997) M2 = rf + Sharpe x sigma_m, and M2 minus the benchmark's "
    "mean return = sigma_m x (Sharpe - Sharpe_m). Aligned daily returns, arithmetic annualisation, daily beta.",
    "information_ratio": "Information ratio = mean(r - r_b) / std(r - r_b) x sqrt(52); tracking error = "
    "std(r - r_b) x sqrt(52); Friday-to-Friday returns, so markets closing at different times stay comparable.",
    "capture": "Up (down) capture: annualised geometric return of the security over the calendar months in which "
    "the benchmark returned at least zero (less than zero), divided by the benchmark's over the same months, x 100. "
    "Month split as in Morningstar Direct's Custom Calculation Data Points (2016); Morningstar describes geometric "
    "averaging without stating its annualisation, so its figures can differ slightly. The month in progress is "
    "excluded.",
    "parkinson": "Parkinson (1980) range estimator, as given in arXiv:2312.01426 Sec. 2: "
    "sigma^2 = (1 / (4 ln 2)) * ln(H/L)^2.",
    "garman_klass": "Garman-Klass (1980) practical estimator, arXiv:2312.01426 eq. (2.1): "
    "sigma^2 = 0.5 ln(H/L)^2 - (2 ln 2 - 1) ln(C/O)^2. Efficiency about 7.4x close-to-close.",
    "rogers_satchell": "Rogers-Satchell (1991) drift-independent estimator, arXiv:2312.01426 Sec. 2: "
    "sigma^2 = ln(H/O) ln(H/C) + ln(L/O) ln(L/C).",
    "tracking": "Tracking difference: annualised fund return minus annualised benchmark return on "
    "the same dates and currency (sign: fund - benchmark). Tracking error: annualised standard "
    "deviation of Friday-to-Friday return differences.",
    "price": "Last close and 52-week high/low from split- and dividend-adjusted daily prices.",
    "sma": "Simple moving average of the last n closes (50 and 200 sessions).",
    "rsi": "RSI, Wilder (1978): smoothing alpha = 1/n.",
    "macd": "MACD (12, 26, 9): EMA12 - EMA26, signal = EMA9 of MACD.",
    "bollinger": "Bollinger bands (20, 2): SMA20 +/- 2 population standard deviations.",
    "atr": "Average True Range, Wilder (1978): Wilder smoothing of max(H-L, |H-C_prev|, |L-C_prev|).",
    "fundamentals": "Ratios from the latest annual statements published by Yahoo Finance, defined as in "
    "the USMx AF6010 'Accounting and Financial Ratios and Formulas' sheet (edX): DuPont ROE = net margin x "
    "asset turnover x equity multiplier on average balances; days = 365 / turnover; days payables on cost "
    "of revenue (the sheet uses operating expenses); cash ratio and operating cash flow coverage on average "
    "current liabilities; CAPM cost of equity, WACC with market equity and book debt, EVA = NOPAT - WACC x "
    "invested capital, Hamada unlevered beta.",
    "piotroski": "Piotroski (2000), J. of Accounting Research 38 (supplement), section 2.3: one point each for ROA > 0, "
    "CFO > 0, higher ROA, CFO > ROA (ROA and CFO over total assets at the start of the year), lower long-term debt "
    "(Yahoo long-term plus current debt) over average total assets, higher current ratio, no common equity issued "
    "(cash flow line, else no rise in shares outstanding), higher gross margin, higher sales over start-of-year total "
    "assets. Null when any signal lacks an input; the partial score is in scores.piotroski.",
    "altman": "Altman (2000), 'Predicting financial distress of companies: revisiting the Z-score and ZETA models': "
    "Z = 1.2 X1 + 1.4 X2 + 3.3 X3 + 0.6 X4 + 1.0 X5 for public manufacturers (X1 working capital, X2 retained "
    "earnings, X3 EBIT, X5 sales, all over total assets; X4 market equity over total liabilities). Altman advocates "
    "1.81 as the cutoff; 2.99 bounds the grey zone from above in the 1968 paper, as commonly cited. Z'' = 6.56 X1 + "
    "3.26 X2 + 6.72 X3 + 1.05 X4 with book equity in X4, for non-manufacturers and emerging markets; the paper read "
    "gives no Z'' cutoffs.",
    "beneish": "Beneish (1999), 'The Detection of Earnings Manipulation', unweighted probit: M = -4.840 + 0.920 DSRI + "
    "0.528 GMI + 0.404 AQI + 0.892 SGI + 0.115 DEPI - 0.172 SGAI + 4.679 TATA - 0.327 LVGI, variables as in its "
    "Table 2. Above -1.78 the model flags a likely manipulator at relative error costs of 20:1 or 30:1 (26% of "
    "manipulators missed and 13.8% of non-manipulators flagged in the estimation sample). AQI, DEPI and SGAI are set "
    "to 1 as the paper does when their inputs are missing; depreciation excludes amortisation, so DEPI is usually 1 "
    "on Yahoo data.",
}
