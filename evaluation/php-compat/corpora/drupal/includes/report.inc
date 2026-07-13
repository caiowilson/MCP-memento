<?php

declare(strict_types=1);

function report_fixture_label(int $count): string
{
    return sprintf('%d reports', $count);
}
