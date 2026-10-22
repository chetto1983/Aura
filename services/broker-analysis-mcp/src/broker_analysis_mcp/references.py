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
    "parkinson": "Parkinson (1980) range estimator, as given in arXiv:2312.01426 Sec. 2: "
    "sigma^2 = (1 / (4 ln 2)) * ln(H/L)^2.",
    "garman_klass": "Garman-Klass (1980) practical estimator, arXiv:2312.01426 eq. (2.1): "
    "sigma^2 = 0.5 ln(H/L)^2 - (2 ln 2 - 1) ln(C/O)^2. Efficiency about 7.4x close-to-close.",
    "rogers_satchell": "Rogers-Satchell (1991) drift-independent estimator, arXiv:2312.01426 Sec. 2: "
    "sigma^2 = ln(H/O) ln(H/C) + ln(L/O) ln(L/C).",
    "tracking": "Tracking difference: annualised fund return minus annualised benchmark return on "
    "the same dates and currency (sign: fund - benchmark). Tracking error: annualised standard "
    "deviation of daily return differences.",
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
}
