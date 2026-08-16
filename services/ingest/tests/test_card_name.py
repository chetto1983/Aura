"""The name filecard is given must route on the converted extension AND read as the name.

Serving only the first is what made a chat attachment describe itself as "tmptq9teunw.pdf".
"""

from ingest.app import _card_name


def test_a_converted_file_keeps_the_real_stem_and_takes_the_parsed_extension():
    assert _card_name("clienti.ods", "/tmp/tmpa3i7cxsi.xlsx") == "clienti.xlsx"


def test_an_unconverted_file_is_named_exactly_as_it_arrived():
    assert _card_name(
        "colm2025_conference.pdf", "/tmp/tmptq9teunw.pdf"
    ) == "colm2025_conference.pdf"


def test_the_temp_path_never_supplies_the_stem():
    # The whole defect in one assertion: the card's first line is indexed and read back by
    # the agent, so a temp stem reaches the model as the document's name.
    assert "tmp" not in _card_name("Perizia città di Ghèdi 2026.txt", "/tmp/tmpa3i7cxsi.txt")


def test_accented_and_spaced_names_survive_intact():
    assert _card_name(
        "Perizia città di Ghèdi 2026.txt", "/tmp/tmpa3i7cxsi.txt"
    ) == "Perizia città di Ghèdi 2026.txt"


def test_a_name_with_no_extension_takes_the_parsed_one():
    assert _card_name("verbale", "/tmp/tmpq1w2e3.pdf") == "verbale.pdf"


def test_only_the_final_extension_is_replaced():
    # "archivio.tar.gz" stems to "archivio.tar": the compound suffix is part of the name a
    # person wrote, and only the extension LibreOffice actually produced is appended.
    assert _card_name("archivio.tar.gz", "/tmp/tmpz9.gz") == "archivio.tar.gz"
