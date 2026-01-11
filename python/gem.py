import yfinance as yf
import pandas as pd
import matplotlib.pyplot as plt
from datetime import datetime, timedelta

# Pobranie danych
tickers = ['EIMI.L', 'CNDX.L', 'CBU0.L', 'IB01.L']
end = datetime.now()
start = end - timedelta(days=365)

data = yf.download(tickers, start=start, end=end)['Close']

# Wypełnienie braków metodą forward fill (ostatnia znana wartość)
data = data.fillna(method='ffill')

# Obliczenie zwrotów procentowych (normalizacja do 0%)
returns = (data / data.iloc[0] - 1) * 100

# Tworzenie wykresu
fig, ax = plt.subplots(figsize=(12, 7))

# Kolory dla poszczególnych linii (na wzór screena)
colors = {
    'EIMI.L': '#0000FF',   # niebieski
    'CNDX.L': '#FFA500',   # pomarańczowy
    'CBU0.L': '#008000',   # zielony
    'IB01.L': '#FF0000'    # czerwony
}

# Rysowanie linii dla każdego ETF-a
for ticker in tickers:
    ax.plot(returns.index, returns[ticker], 
            color=colors[ticker], linewidth=1.5, label=ticker)

# Linia zerowa (przerywana)
ax.axhline(y=0, color='gray', linestyle='--', linewidth=0.8, alpha=0.7)

# Siatka
ax.grid(True, alpha=0.3, linestyle='-', linewidth=0.5)

# Formatowanie osi Y - tylko po prawej stronie
# Dodaj więcej miejsca u góry (zwiększ górny limit)
max_value = returns.max().max()
y_margin = (max_value + 25) * 0.15  # 15% marginesu
ax.set_ylim(-25, max_value + y_margin)
ax.set_yticks([])  # Usunięcie lewej osi
ax.spines['left'].set_visible(False)

# Dodanie skali po prawej stronie
ax2 = ax.twinx()
ax2.set_ylim(-25, max_value + y_margin)
yticks = range(-20, int(max_value + y_margin) + 5, 10)
ax2.set_yticks(yticks)
ax2.set_yticklabels([f'{y}%' for y in yticks])

# Dodanie etykiet na końcu linii - w stylu kolorowych boxów
for ticker in tickers:
    last_value = returns[ticker].iloc[-1]
    # Kolorowy box z wartością
    bbox_props = dict(boxstyle='round,pad=0.4', 
                     facecolor=colors[ticker], 
                     edgecolor='none', 
                     alpha=0.9)
    ax2.text(1.01, last_value, f'{last_value:.1f}%', 
             color='white', fontsize=9, va='center', ha='left',
             weight='bold', transform=ax2.get_yaxis_transform(),
             clip_on=False, bbox=bbox_props)

# Formatowanie osi X
months_pl = ['Sty', 'Lut', 'Mar', 'Kwi', 'Maj', 'Cze', 
             'Lip', 'Sie', 'Wrz', 'Paź', 'Lis', 'Gru']
ax.set_xlabel('Interwał Dzienny', fontsize=9, loc='right')

# Rozszerzenie osi X żeby wykres kończył się przy osi
x_range = (returns.index[-1] - returns.index[0]).days
extension = timedelta(days=x_range * 0.02)  # 2% rozszerzenia dla etykiet
ax.set_xlim(returns.index[0], returns.index[-1] + extension)

# Ustawienie etykiet miesięcy na osi X z rokiem
month_positions = pd.date_range(start=start, end=end, freq='MS')
ax.set_xticks(month_positions)
month_labels = []
for d in month_positions:
    # Dodaj rok tylko dla stycznia lub pierwszego miesiąca
    if d.month == 1 or d == month_positions[0]:
        month_labels.append(f"{months_pl[d.month-1]} '{d.year % 100:02d}")
    else:
        month_labels.append(months_pl[d.month-1])
ax.set_xticklabels(month_labels, fontsize=9)

# Tytuł z datą
date_str = end.strftime('%d %b %Y %H:%M UTC+1')
title_text = f'Porównanie ETF - 1 rok                    {date_str}               '
ax.set_title(title_text, fontsize=11, loc='left', pad=10)

# Legenda ze stopami zwrotu
legend_labels = []
for ticker in tickers:
    year_return = returns[ticker].iloc[-1]
    legend_labels.append(f'{ticker}: {year_return:+.2f}%')

ax.legend(legend_labels, loc='upper left', fontsize=9, framealpha=0.9)

# Wyświetlenie stóp zwrotu w konsoli
print("\n" + "="*60)
print("STOPY ZWROTU - 1 ROK:")
print("="*60)
for ticker in tickers:
    year_return = returns[ticker].iloc[-1]
    color_name = {
        '#0000FF': 'niebieski',
        '#FFA500': 'pomarańczowy', 
        '#008000': 'zielony',
        '#FF0000': 'czerwony'
    }[colors[ticker]]
    
    print(f"{ticker:10} ({color_name:12}): {year_return:+7.2f}%")
print("="*60 + "\n")

# Dopasowanie layoutu
plt.tight_layout()
plt.subplots_adjust(top=0.93)

# Zapisanie wykresu
plt.savefig('etfs_rok.png', dpi=150, bbox_inches='tight', facecolor='white')
print("Wykres zapisany jako: etfs_rok.png")