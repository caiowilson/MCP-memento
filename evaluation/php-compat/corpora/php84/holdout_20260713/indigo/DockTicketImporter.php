<?php
declare(strict_types=1);

namespace Holdout\Php84\Indigo;

final class DockTicketImporter
{
    public function import(string $rawCode): DockTicket
    {
        $ticket = new DockTicket();
        $ticket->code = $rawCode;

        return $ticket;
    }
}
